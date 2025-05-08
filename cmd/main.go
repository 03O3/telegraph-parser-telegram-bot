package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"telegraph-finder-go/parser"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

func main() {
	// Парсинг аргументов командной строки
	queryFlag := flag.String("q", "", "Поисковый запрос")
	urlFlag := flag.String("u", "", "Конкретная ссылка на Telegraph для проверки")
	accountsFlag := flag.Bool("accounts", false, "Искать аккаунты в найденных статьях")
	webhooksFlag := flag.Bool("webhooks", false, "Искать вебхуки в найденных статьях")
	accountsTypeFlag := flag.String("type", "", "Тип аккаунтов для поиска (minecraft, email, all)")
	webhookTypeFlag := flag.String("webhook-type", "", "Тип вебхуков для поиска (discord, github, slack, generic, all)")
	outputFlag := flag.String("o", "results.txt", "Файл для сохранения найденных данных")

	// Параметры многопоточности и конфигурации
	concurrentRequestsFlag := flag.Int("concurrent", 10, "Максимальное количество одновременных запросов")
	analyzeWorkersFlag := flag.Int("analyze-workers", 8, "Количество параллельных процессов для анализа результатов")
	timeoutFlag := flag.Int("timeout", 10, "Таймаут HTTP запросов в секундах")
	retryCountFlag := flag.Int("retry", 3, "Количество повторных попыток при ошибке")
	delayFlag := flag.Int("delay", 100, "Задержка между запросами в миллисекундах")
	monthsFlag := flag.String("months", "", "Месяцы для поиска (через запятую, например: 1,2,3)")
	noTranslitFlag := flag.Bool("no-translit", false, "Отключить транслитерацию запроса")

	flag.Parse()

	// Создаем конфигурацию парсера
	config := parser.DefaultConfig()

	// Применяем настройки из флагов командной строки
	config.MaxConcurrentRequests = int64(*concurrentRequestsFlag)
	config.RequestTimeout = time.Duration(*timeoutFlag) * time.Second
	config.RetryCount = *retryCountFlag
	config.DelayBetweenRequests = time.Duration(*delayFlag) * time.Millisecond
	config.IncludeTranslitVariants = !*noTranslitFlag

	// Парсим месяцы
	if *monthsFlag != "" {
		monthStrings := strings.Split(*monthsFlag, ",")
		months := make([]int, 0, len(monthStrings))

		for _, monthStr := range monthStrings {
			month, err := strconv.Atoi(strings.TrimSpace(monthStr))
			if err == nil && month > 0 && month <= 12 {
				months = append(months, month)
			}
		}

		if len(months) > 0 {
			config.MonthsToSearch = months
		}
	}

	// Создаем контекст с возможностью отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Проверка конкретной ссылки на наличие вебхуков
	if *urlFlag != "" && *webhooksFlag {
		fmt.Printf("Поиск вебхуков в: %s\n", *urlFlag)
		webhooks, err := parser.FindWebhooksForSpecificURL(ctx, *urlFlag)

		if err != nil {
			fmt.Printf("Ошибка при проверке ссылки: %v\n", err)
			os.Exit(1)
		}

		if len(webhooks) > 0 {
			fmt.Printf("Найдено %d вебхуков:\n", len(webhooks))
			displayWebhooks(webhooks, *webhookTypeFlag)
			saveWebhooksToFile(webhooks, *outputFlag, *webhookTypeFlag)
		} else {
			fmt.Println("Вебхуки не найдены")
		}
		os.Exit(0)
	}

	// Проверка конкретной ссылки на наличие аккаунтов
	if *urlFlag != "" && *accountsFlag {
		fmt.Printf("Поиск аккаунтов в: %s\n", *urlFlag)
		accounts, err := parser.FindAccountsForSpecificURL(ctx, *urlFlag)

		if err != nil {
			fmt.Printf("Ошибка при проверке ссылки: %v\n", err)
			os.Exit(1)
		}

		if len(accounts) > 0 {
			fmt.Printf("Найдено %d аккаунтов:\n", len(accounts))
			displayAccounts(accounts, *accountsTypeFlag)
			saveAccountsToFile(accounts, *outputFlag, *accountsTypeFlag)
		} else {
			fmt.Println("Аккаунты не найдены")
		}
		os.Exit(0)
	}

	// Обычная проверка ссылки
	if *urlFlag != "" {
		fmt.Printf("Проверка ссылки: %s\n", *urlFlag)
		result, err := parser.FindArticlesForSpecificURL(ctx, *urlFlag)

		if err != nil {
			fmt.Printf("Ошибка при проверке ссылки: %v\n", err)
			os.Exit(1)
		}

		if result != "" {
			fmt.Println("Ссылка доступна и содержит контент:")
			fmt.Println(result)
		} else {
			fmt.Println("Ссылка недоступна или содержит недопустимый контент")
		}
		os.Exit(0)
	}

	// Проверка наличия поискового запроса
	query := *queryFlag
	if query == "" {
		// Если флаг -q не был использован, проверяем аргументы
		args := flag.Args()
		if len(args) < 1 {
			fmt.Println("Использование:")
			fmt.Println("  telegraph-finder -q <запрос> [-accounts] [-webhooks] [-type <тип>] [-webhook-type <тип>] [-o <файл>] - Поиск статей и данных")
			fmt.Println("  telegraph-finder -u <ссылка> [-accounts] [-webhooks] [-o <файл>] - Проверка ссылки")
			fmt.Println("  telegraph-finder <запрос> - Поиск статей")
			fmt.Println("\nПараметры многопоточности:")
			fmt.Println("  -concurrent <N> - Максимальное количество одновременных запросов (по умолчанию: 10)")
			fmt.Println("  -analyze-workers <N> - Количество параллельных процессов для анализа результатов (по умолчанию: 8)")
			fmt.Println("  -timeout <N> - Таймаут HTTP запросов в секундах (по умолчанию: 10)")
			fmt.Println("  -retry <N> - Количество повторных попыток при ошибке (по умолчанию: 3)")
			fmt.Println("  -delay <N> - Задержка между запросами в миллисекундах (по умолчанию: 100)")
			fmt.Println("  -months <месяцы> - Месяцы для поиска через запятую (например: 1,5,9)")
			fmt.Println("  -no-translit - Отключить транслитерацию запроса")
			os.Exit(1)
		}
		query = strings.Join(args, " ")
	}

	fmt.Printf("Поиск статей для запроса: %s\n", query)
	if config.IncludeTranslitVariants {
		fmt.Printf("Транслитерированный запрос: %s\n", parser.Translit(query))
	}

	// Вывод информации о конфигурации
	fmt.Printf("Конфигурация: %d параллельных запросов, таймаут %v, задержка %v\n",
		config.MaxConcurrentRequests, config.RequestTimeout, config.DelayBetweenRequests)

	if len(config.MonthsToSearch) > 0 {
		fmt.Printf("Поиск по месяцам: %v\n", config.MonthsToSearch)
	}

	fmt.Println("Начинаю поиск, это может занять некоторое время...")

	// Запускаем поиск статей с функцией обратного вызова для отображения прогресса
	startTime := time.Now()
	results, err := parser.FindArticlesWithConfig(ctx, query, config, func(current, total int) {
		percent := int(float64(current) / float64(total) * 100)
		progressBar := createProgressBar(percent, 20) // 20 символов в полоске прогресса
		fmt.Printf("\rПрогресс: [%s] %d/%d задач (%d%%) ", progressBar, current, total, percent)
	})

	// Печатаем новую строку после завершения прогресса
	fmt.Println()

	if err != nil {
		fmt.Printf("Ошибка при поиске: %v\n", err)
		os.Exit(1)
	}

	// Выводим статистику
	duration := time.Since(startTime).Round(time.Second)
	rate := float64(len(results)) / duration.Seconds()
	fmt.Println("Поиск завершен!")
	fmt.Printf("Найдено %d статей за %s (%.2f статей/сек)\n", len(results), duration, rate)

	if len(results) > 0 {
		fmt.Println("\nНайденные статьи:")
		for i, article := range results {
			fmt.Printf("%d. %s\n", i+1, article)
		}

		// Если включен флаг поиска вебхуков или аккаунтов, запускаем параллельный анализ
		if *webhooksFlag || *accountsFlag {
			fmt.Println("\nНачинаю анализ найденных статей...")

			// Запускаем параллельный анализ результатов
			startAnalyzeTime := time.Now()
			allAccounts, allWebhooks, err := parallelAnalyzeResults(ctx, results, *analyzeWorkersFlag, *accountsFlag, *webhooksFlag)

			if err != nil {
				fmt.Printf("Ошибка при анализе: %v\n", err)
			} else {
				analyzeDuration := time.Since(startAnalyzeTime).Round(time.Second)
				fmt.Printf("Анализ завершен за %s\n", analyzeDuration)

				// Обработка результатов поиска аккаунтов
				if *accountsFlag && len(allAccounts) > 0 {
					fmt.Printf("\nВсего найдено %d аккаунтов\n", len(allAccounts))
					displayAccounts(allAccounts, *accountsTypeFlag)
					saveAccountsToFile(allAccounts, *outputFlag+".accounts", *accountsTypeFlag)
				} else if *accountsFlag {
					fmt.Println("\nАккаунты не найдены")
				}

				// Обработка результатов поиска вебхуков
				if *webhooksFlag && len(allWebhooks) > 0 {
					fmt.Printf("\nВсего найдено %d вебхуков\n", len(allWebhooks))
					displayWebhooks(allWebhooks, *webhookTypeFlag)
					saveWebhooksToFile(allWebhooks, *outputFlag+".webhooks", *webhookTypeFlag)
				} else if *webhooksFlag {
					fmt.Println("\nВебхуки не найдены")
				}
			}
		}
	} else {
		fmt.Println("Статьи не найдены. Попробуйте другой запрос.")
	}
}

// displayWebhooks отображает найденные вебхуки
func displayWebhooks(webhooks []parser.WebhookData, typeFilter string) {
	for i, wh := range webhooks {
		if typeFilter != "" && typeFilter != "all" && wh.Type != typeFilter {
			continue
		}
		fmt.Printf("%d. [%s] %s (%s)\n", i+1, wh.Type, wh.URL, wh.Source)
	}
}

// saveWebhooksToFile сохраняет вебхуки в файл
func saveWebhooksToFile(webhooks []parser.WebhookData, filename string, typeFilter string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Форматируем и фильтруем вебхуки
	var filteredWebhooks []parser.WebhookData
	for _, wh := range webhooks {
		if typeFilter != "" && typeFilter != "all" && wh.Type != typeFilter {
			continue
		}
		filteredWebhooks = append(filteredWebhooks, wh)
	}

	// Сохраняем в текстовом формате
	for i, wh := range filteredWebhooks {
		_, err := fmt.Fprintf(file, "%d. [%s] %s (%s)\n", i+1, wh.Type, wh.URL, wh.Source)
		if err != nil {
			return err
		}
	}

	// Дополнительно сохраняем в JSON
	jsonFile, err := os.Create(filename + ".json")
	if err != nil {
		return err
	}
	defer jsonFile.Close()

	encoder := json.NewEncoder(jsonFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(filteredWebhooks)
}

// displayAccounts отображает найденные аккаунты
func displayAccounts(accounts []parser.Account, typeFilter string) {
	for i, acc := range accounts {
		if typeFilter != "" && typeFilter != "all" && acc.Type != typeFilter {
			continue
		}
		fmt.Printf("%d. [%s] %s:%s (%s)\n", i+1, acc.Type, acc.Username, acc.Password, acc.Source)
	}
}

// saveAccountsToFile сохраняет аккаунты в файл
func saveAccountsToFile(accounts []parser.Account, filename string, typeFilter string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Форматируем и фильтруем аккаунты
	var filteredAccounts []parser.Account
	for _, acc := range accounts {
		if typeFilter != "" && typeFilter != "all" && acc.Type != typeFilter {
			continue
		}
		filteredAccounts = append(filteredAccounts, acc)
	}

	// Сохраняем в текстовом формате
	for i, acc := range filteredAccounts {
		_, err := fmt.Fprintf(file, "%d. [%s] %s:%s (%s)\n", i+1, acc.Type, acc.Username, acc.Password, acc.Source)
		if err != nil {
			return err
		}
	}

	// Дополнительно сохраняем в JSON
	jsonFile, err := os.Create(filename + ".json")
	if err != nil {
		return err
	}
	defer jsonFile.Close()

	encoder := json.NewEncoder(jsonFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(filteredAccounts)
}

// createProgressBar создает текстовую полоску прогресса определенной длины
func createProgressBar(percent int, width int) string {
	completed := width * percent / 100
	if completed > width {
		completed = width
	}

	bar := strings.Repeat("█", completed) + strings.Repeat("░", width-completed)
	return bar
}

// parallelAnalyzeResults параллельно анализирует найденные статьи на предмет аккаунтов или вебхуков
func parallelAnalyzeResults(ctx context.Context, results []string, maxWorkers int,
	findAccounts bool, findWebhooks bool) ([]parser.Account, []parser.WebhookData, error) {

	var allAccounts []parser.Account
	var allWebhooks []parser.WebhookData
	var accountsMu, webhooksMu sync.Mutex

	// Создаем семафор для ограничения количества параллельных запросов
	sem := semaphore.NewWeighted(int64(maxWorkers))

	// Создаем группу ошибок для синхронизации горутин
	g, gctx := errgroup.WithContext(ctx)

	fmt.Printf("Начинаю анализ %d статей с использованием %d параллельных процессов...\n",
		len(results), maxWorkers)

	// Статус прогресса
	var processed int32
	totalArticles := len(results)

	// Запускаем горутину для отображения прогресса
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt32(&processed)
				percent := int(float64(current) / float64(totalArticles) * 100)
				progressBar := createProgressBar(percent, 20)
				fmt.Printf("\rАнализ статей: [%s] %d/%d (%d%%) ",
					progressBar, current, totalArticles, percent)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Обрабатываем каждую статью параллельно
	for _, article := range results {
		// Создаем локальную копию для горутины
		article := article // Используем теневую переменную для избежания захвата итерационной переменной

		// Извлекаем URL из строки результата
		parts := strings.Split(article, " - ")
		if len(parts) < 2 {
			atomic.AddInt32(&processed, 1)
			continue
		}

		url := parts[len(parts)-1]

		g.Go(func() error {
			// Приобретаем семафор
			if err := sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			// Поиск аккаунтов
			if findAccounts {
				accounts, err := parser.ExtractAccounts(url)
				if err == nil && len(accounts) > 0 {
					accountsMu.Lock()
					allAccounts = append(allAccounts, accounts...)
					accountsMu.Unlock()
					fmt.Printf("\nНайдено %d аккаунтов в %s\n", len(accounts), url)
				}
			}

			// Поиск вебхуков
			if findWebhooks {
				webhooks, err := parser.ExtractWebhooks(url)
				if err == nil && len(webhooks) > 0 {
					webhooksMu.Lock()
					allWebhooks = append(allWebhooks, webhooks...)
					webhooksMu.Unlock()
					fmt.Printf("\nНайдено %d вебхуков в %s\n", len(webhooks), url)
				}
			}

			// Увеличиваем счетчик обработанных статей
			atomic.AddInt32(&processed, 1)

			return nil
		})
	}

	// Ожидаем завершения всех горутин
	if err := g.Wait(); err != nil {
		return allAccounts, allWebhooks, err
	}

	// Печатаем новую строку после завершения прогресса
	fmt.Println()

	return allAccounts, allWebhooks, nil
}
