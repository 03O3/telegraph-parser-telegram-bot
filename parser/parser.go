package parser

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// ParserConfig содержит конфигурацию парсера
type ParserConfig struct {
	MaxConcurrentRequests   int64         // Максимальное количество одновременных HTTP запросов
	RequestTimeout          time.Duration // Таймаут HTTP запросов
	RetryCount              int           // Количество повторных попыток при ошибке
	RetryDelay              time.Duration // Задержка между повторными попытками
	DelayBetweenRequests    time.Duration // Задержка между запросами (для избежания блокировки)
	YearsToSearch           []int         // Годы для поиска
	MonthsToSearch          []int         // Месяцы для поиска (1-12, если пусто - все месяцы)
	IncludeTranslitVariants bool          // Включать ли транслитерированные варианты запроса
}

// DefaultConfig возвращает конфигурацию парсера по умолчанию
func DefaultConfig() ParserConfig {
	return ParserConfig{
		MaxConcurrentRequests:   10,
		RequestTimeout:          10 * time.Second,
		RetryCount:              3,
		RetryDelay:              500 * time.Millisecond,
		DelayBetweenRequests:    100 * time.Millisecond,
		YearsToSearch:           []int{}, // Не используется
		MonthsToSearch:          []int{},
		IncludeTranslitVariants: true,
	}
}

// AccountPattern содержит паттерны для поиска учетных данных
var (
	EmailPassPattern = regexp.MustCompile(`([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})[\s:]+([^\s]{3,})`)
	AccountPattern   = regexp.MustCompile(`(account|login|username|user|email)[\s:]+([^\s]+)[\s:]+.*(password|pass|pwd)[\s:]+([^\s]{3,})`)
	MinecraftPattern = regexp.MustCompile(`(minecraft|mc)[\s:]*([^\s:@]+)[\s:]+([^\s]{3,})`)

	// Паттерны для вебхуков
	DiscordWebhookPattern = regexp.MustCompile(`https?://(?:(?:canary|ptb)\.)?discord(?:app)?\.com/api/webhooks/([0-9]{17,20})/([A-Za-z0-9\-_]{60,68})`)
	GitHubWebhookPattern  = regexp.MustCompile(`https?://api\.github\.com/repos/[^/]+/[^/]+/hooks/[0-9]+\?token=([A-Za-z0-9_\-]+)`)
	SlackWebhookPattern   = regexp.MustCompile(`https?://hooks\.slack\.com/services/T[a-zA-Z0-9_]+/B[a-zA-Z0-9_]+/[a-zA-Z0-9_]+`)
	GenericWebhookPattern = regexp.MustCompile(`webhook[s]?[\s:=]+(https?://[a-zA-Z0-9\.\-_/\?=&%]+)`)
)

// WebhookData представляет найденный вебхук
type WebhookData struct {
	Type   string // discord, github, slack, generic
	URL    string
	Source string // URL источника
}

// Account представляет найденные учетные данные
type Account struct {
	Type     string // тип аккаунта (minecraft, email и т.д.)
	Username string
	Password string
	Source   string // URL источника
}

// IgnoreList содержит слова, которые игнорируются в результатах поиска
var IgnoreList = []string{
	"https://t.me/SLlV_INTIM_BOT",
	"https://t.me/+YztOEovieQIzZjY8",
	"free vpn infinite time",
	"mdisk",
	"free exploits",
	"http://openroadmdnzgrna5lzkkjlqvc662o4xbgsjqi22qjek6adq4j6emaad.onion/",
}

// Транслитерация русских букв в английские
var translitMap = map[string]string{
	"а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ж": "zh",
	"з": "z", "и": "i", "й": "j", "к": "k", "л": "l", "м": "m", "н": "n",
	"о": "o", "п": "p", "р": "r", "с": "s", "т": "t", "у": "u", "ф": "f",
	"х": "h", "ц": "ts", "ч": "ch", "ш": "sh", "щ": "shch", "ъ": "",
	"ы": "y", "ь": "", "э": "eh", "ю": "yu", "я": "ya", " ": "-", "ё": "yo",
}

// Translit преобразует русский текст в транслитерацию
func Translit(text string) string {
	text = strings.ToLower(text)
	result := ""

	for _, char := range text {
		if val, ok := translitMap[string(char)]; ok {
			result += val
		} else if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			result += string(char)
		}
	}

	return result
}

// FindArticle проверяет, существует ли статья по заданному URL и не содержит ли она игнорируемых слов
func FindArticle(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Проверка HTTP статуса
	if resp.StatusCode != 200 {
		return "", nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	// Проверка на 404 страницу (Telegraph возвращает 200 для некоторых несуществующих страниц)
	title := doc.Find("title").Text()
	if title == "404 Not Found" || title == "Telegraph" || title == "" {
		return "", nil
	}

	// Проверка содержимого страницы
	content := doc.Find("article").Text()
	if content == "" || len(strings.TrimSpace(content)) < 50 {
		// Страница пустая или слишком короткая
		return "", nil
	}

	// Проверка на наличие игнорируемых слов
	html, err := doc.Html()
	if err != nil {
		return "", err
	}

	for _, ignoreWord := range IgnoreList {
		if strings.Contains(html, ignoreWord) {
			return "", nil
		}
	}

	// Формируем результат с заголовком статьи
	result := fmt.Sprintf("%s - %s", title, url)
	return result, nil
}

// ExtractAccounts извлекает учетные данные из контента страницы
func ExtractAccounts(url string) ([]Account, error) {
	var accounts []Account

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	content := doc.Find("article").Text()

	// Поиск email:pass паттернов
	emailMatches := EmailPassPattern.FindAllStringSubmatch(content, -1)
	for _, match := range emailMatches {
		if len(match) >= 3 {
			accounts = append(accounts, Account{
				Type:     "email",
				Username: match[1],
				Password: match[2],
				Source:   url,
			})
		}
	}

	// Поиск Minecraft аккаунтов
	mcMatches := MinecraftPattern.FindAllStringSubmatch(content, -1)
	for _, match := range mcMatches {
		if len(match) >= 4 {
			accounts = append(accounts, Account{
				Type:     "minecraft",
				Username: match[2],
				Password: match[3],
				Source:   url,
			})
		}
	}

	// Поиск общего формата аккаунтов
	accMatches := AccountPattern.FindAllStringSubmatch(content, -1)
	for _, match := range accMatches {
		if len(match) >= 5 {
			accounts = append(accounts, Account{
				Type:     strings.ToLower(match[1]),
				Username: match[2],
				Password: match[4],
				Source:   url,
			})
		}
	}

	return accounts, nil
}

// FindAccountsInArticle ищет учетные данные в статье
func FindAccountsInArticle(client *http.Client, url string) ([]Account, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Проверка на 404 страницу
	title := doc.Find("title").Text()
	if title == "404 Not Found" || title == "Telegraph" || title == "" {
		return nil, nil
	}

	// Получаем контент страницы
	content := doc.Find("article").Text()
	if content == "" || len(strings.TrimSpace(content)) < 50 {
		return nil, nil
	}

	var accounts []Account

	// Поиск email:pass паттернов
	emailMatches := EmailPassPattern.FindAllStringSubmatch(content, -1)
	for _, match := range emailMatches {
		if len(match) >= 3 {
			accounts = append(accounts, Account{
				Type:     "email",
				Username: match[1],
				Password: match[2],
				Source:   url,
			})
		}
	}

	// Поиск Minecraft аккаунтов
	mcMatches := MinecraftPattern.FindAllStringSubmatch(content, -1)
	for _, match := range mcMatches {
		if len(match) >= 4 {
			accounts = append(accounts, Account{
				Type:     "minecraft",
				Username: match[2],
				Password: match[3],
				Source:   url,
			})
		}
	}

	// Поиск общего формата аккаунтов
	accMatches := AccountPattern.FindAllStringSubmatch(content, -1)
	for _, match := range accMatches {
		if len(match) >= 5 {
			accounts = append(accounts, Account{
				Type:     strings.ToLower(match[1]),
				Username: match[2],
				Password: match[4],
				Source:   url,
			})
		}
	}

	return accounts, nil
}

// FindArticlesForDay ищет статьи за указанный день месяца
func FindArticlesForDay(ctx context.Context, client *http.Client, query, month, day string, year int) ([]string, error) {
	var results []string
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	// Проверяем статью без индекса
	g.Go(func() error {
		select {
		case <-gctx.Done():
			return gctx.Err()
		default:
			// продолжаем выполнение
		}

		// В Telegraph URL обычно не содержит год
		url := fmt.Sprintf("https://telegra.ph/%s-%s-%s", query, month, day)
		if article, err := FindArticle(client, url); err == nil && article != "" {
			mu.Lock()
			results = append(results, article)
			mu.Unlock()
		}

		return nil
	})

	// Проверяем статьи с индексами от 2 до 30
	for i := 2; i <= 30; i++ {
		index := i // Создаем локальную переменную для горутины
		g.Go(func() error {
			time.Sleep(time.Duration(index-1) * 100 * time.Millisecond) // Задержка для предотвращения блокировки

			// В Telegraph URL обычно содержит только индекс, но не год
			url := fmt.Sprintf("https://telegra.ph/%s-%s-%s-%d", query, month, day, index)
			if article, err := FindArticle(client, url); err == nil && article != "" {
				mu.Lock()
				results = append(results, article)
				mu.Unlock()
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// FindArticlesForMonth ищет статьи за указанный месяц
// Параметр year не используется в формировании URL, но сохраняется для совместимости
func FindArticlesForMonth(ctx context.Context, client *http.Client, query string, month, year int) ([]string, error) {
	var results []string
	var mu sync.Mutex
	g := errgroup.Group{}

	monthStr := fmt.Sprintf("%02d", month)

	// Проверяем каждый день месяца
	for day := 1; day <= 31; day++ {
		dayStr := fmt.Sprintf("%02d", day)
		day := day // Создаем локальную копию для горутины
		g.Go(func() error {
			time.Sleep(time.Duration(day-1) * 50 * time.Millisecond) // Небольшая задержка
			// Параметр year передается, но не используется в URL
			articles, err := FindArticlesForDay(ctx, client, query, monthStr, dayStr, year)
			if err != nil {
				return err
			}

			if len(articles) > 0 {
				mu.Lock()
				results = append(results, articles...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// FindArticlesWithConfig ищет все статьи по запросу с использованием указанной конфигурации
// Параметр годов сохраняется для совместимости, но не используется
func FindArticlesWithConfig(ctx context.Context, query string, config ParserConfig, progressCallback func(int, int)) ([]string, error) {
	queries := []string{query}

	// Если включена опция транслитерации, добавляем транслитерированный вариант
	if config.IncludeTranslitVariants {
		translitQuery := Translit(query)
		if translitQuery != query {
			queries = append(queries, translitQuery)
		}
	}

	// Создаем HTTP клиент с настроенным таймаутом
	client := &http.Client{
		Timeout: config.RequestTimeout,
	}

	var results []string
	var mu sync.Mutex

	// Атомарный счетчик для отслеживания прогресса
	var processedTasks int32

	// Если месяцы не указаны, используем все (1-12)
	months := config.MonthsToSearch
	if len(months) == 0 {
		months = make([]int, 12)
		for i := 0; i < 12; i++ {
			months[i] = i + 1
		}
	}

	// Год не влияет на URL в Telegraph, но нужен как параметр для совместимости
	currentYear := time.Now().Year()

	// Вычисляем общее количество задач (месяц*запрос)
	totalTasks := len(months) * len(queries)

	// Создаем семафор для ограничения количества параллельных запросов
	sem := semaphore.NewWeighted(config.MaxConcurrentRequests)

	// Запускаем горутину для регулярного обновления прогресса
	if progressCallback != nil {
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					current := atomic.LoadInt32(&processedTasks)
					progressCallback(int(current), totalTasks)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Создаем группу ошибок для синхронизации горутин
	g, gctx := errgroup.WithContext(ctx)

	// Для каждого запроса и месяца создаем отдельную задачу
	for _, searchQuery := range queries {
		for _, month := range months {
			// Создаем локальные копии переменных для горутин
			q := searchQuery
			m := month

			g.Go(func() error {
				// Приобретаем блокировку семафора
				if err := sem.Acquire(gctx, 1); err != nil {
					return err
				}
				defer sem.Release(1)

				// Вносим задержку для предотвращения блокировки сервера
				time.Sleep(config.DelayBetweenRequests)

				// Выполняем поиск для конкретного месяца
				// Год передается как параметр, но не используется в URL
				monthArticles, err := FindArticlesForMonth(gctx, client, q, m, currentYear)

				// Увеличиваем счетчик обработанных задач
				atomic.AddInt32(&processedTasks, 1)

				if err != nil {
					return err
				}

				// Если найдены статьи, добавляем их в общий список
				if len(monthArticles) > 0 {
					mu.Lock()
					results = append(results, monthArticles...)
					mu.Unlock()
				}

				return nil
			})
		}
	}

	// Ожидаем завершения всех горутин
	if err := g.Wait(); err != nil {
		return results, err
	}

	return results, nil
}

// FindArticles вызывает FindArticlesWithConfig с конфигурацией по умолчанию
func FindArticles(ctx context.Context, query string, progressCallback func(int, int)) ([]string, error) {
	return FindArticlesWithConfig(ctx, query, DefaultConfig(), progressCallback)
}

// FindArticlesForSpecificURL проверяет конкретную ссылку на Telegraph
func FindArticlesForSpecificURL(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return FindArticle(client, url)
}

// FindAccountsForSpecificURL проверяет конкретную ссылку на наличие учетных данных
func FindAccountsForSpecificURL(ctx context.Context, url string) ([]Account, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return FindAccountsInArticle(client, url)
}

// FindWebhooksInArticle ищет вебхуки в статье
func FindWebhooksInArticle(client *http.Client, url string) ([]WebhookData, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Проверка на 404 страницу
	title := doc.Find("title").Text()
	if title == "404 Not Found" || title == "Telegraph" || title == "" {
		return nil, nil
	}

	// Получаем контент страницы
	content := doc.Find("article").Text()
	if content == "" || len(strings.TrimSpace(content)) < 50 {
		return nil, nil
	}

	var webhooks []WebhookData

	// Поиск Discord вебхуков
	discordMatches := DiscordWebhookPattern.FindAllStringSubmatch(content, -1)
	for _, match := range discordMatches {
		if len(match) > 0 {
			webhooks = append(webhooks, WebhookData{
				Type:   "discord",
				URL:    match[0],
				Source: url,
			})
		}
	}

	// Поиск GitHub вебхуков
	githubMatches := GitHubWebhookPattern.FindAllStringSubmatch(content, -1)
	for _, match := range githubMatches {
		if len(match) > 0 {
			webhooks = append(webhooks, WebhookData{
				Type:   "github",
				URL:    match[0],
				Source: url,
			})
		}
	}

	// Поиск Slack вебхуков
	slackMatches := SlackWebhookPattern.FindAllStringSubmatch(content, -1)
	for _, match := range slackMatches {
		if len(match) > 0 {
			webhooks = append(webhooks, WebhookData{
				Type:   "slack",
				URL:    match[0],
				Source: url,
			})
		}
	}

	// Поиск обобщенных вебхуков
	genericMatches := GenericWebhookPattern.FindAllStringSubmatch(content, -1)
	for _, match := range genericMatches {
		if len(match) > 1 {
			// Проверяем, что вебхук не совпадает с уже найденными
			isDuplicate := false
			for _, wh := range webhooks {
				if strings.Contains(match[1], wh.URL) || strings.Contains(wh.URL, match[1]) {
					isDuplicate = true
					break
				}
			}

			if !isDuplicate {
				webhooks = append(webhooks, WebhookData{
					Type:   "generic",
					URL:    match[1],
					Source: url,
				})
			}
		}
	}

	return webhooks, nil
}

// ExtractWebhooks извлекает вебхуки из контента страницы
func ExtractWebhooks(url string) ([]WebhookData, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return FindWebhooksInArticle(client, url)
}

// FindWebhooksForSpecificURL проверяет конкретную ссылку на наличие вебхуков
func FindWebhooksForSpecificURL(ctx context.Context, url string) ([]WebhookData, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return FindWebhooksInArticle(client, url)
}
