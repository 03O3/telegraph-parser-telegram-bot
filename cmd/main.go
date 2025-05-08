package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"telegraph-finder-go/parser"
)

func main() {
	// Парсинг аргументов командной строки
	queryFlag := flag.String("q", "", "Поисковый запрос")
	urlFlag := flag.String("u", "", "Конкретная ссылка на Telegraph для проверки")
	flag.Parse()

	// Создаем контекст с возможностью отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Проверка конкретной ссылки, если она указана
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
			fmt.Println("Использование: telegraph-finder -q <запрос> или -u <ссылка> или telegraph-finder <запрос>")
			os.Exit(1)
		}
		query = strings.Join(args, " ")
	}

	fmt.Printf("Поиск статей для запроса: %s\n", query)
	fmt.Printf("Транслитерированный запрос: %s\n", parser.Translit(query))
	fmt.Println("Начинаю поиск, это может занять некоторое время...")

	// Запускаем поиск статей с функцией обратного вызова для отображения прогресса
	startTime := time.Now()
	results, err := parser.FindArticles(ctx, query, func(current, total int) {
		fmt.Printf("Прогресс: %d/%d месяцев\r", current, total)
	})

	if err != nil {
		fmt.Printf("Ошибка при поиске: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nПоиск завершен!")
	fmt.Printf("Найдено %d статей за %s\n", len(results), time.Since(startTime).Round(time.Second))

	if len(results) > 0 {
		fmt.Println("\nНайденные статьи:")
		for i, article := range results {
			fmt.Printf("%d. %s\n", i+1, article)
		}
	} else {
		fmt.Println("Статьи не найдены. Попробуйте другой запрос.")
	}
}
