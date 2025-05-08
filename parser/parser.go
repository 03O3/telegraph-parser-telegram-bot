package parser

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

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

// FindArticlesForDay ищет статьи за указанный день месяца и года
func FindArticlesForDay(ctx context.Context, client *http.Client, query, month, day string, year int) ([]string, error) {
	var results []string
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	yearStr := fmt.Sprintf("%d", year%100) // Берем последние две цифры года для URL

	// Проверяем статью без индекса
	g.Go(func() error {
		select {
		case <-gctx.Done():
			return gctx.Err()
		default:
			// продолжаем выполнение
		}

		url := fmt.Sprintf("https://telegra.ph/%s-%s-%s-%s", query, month, day, yearStr)
		if article, err := FindArticle(client, url); err == nil && article != "" {
			mu.Lock()
			results = append(results, article)
			mu.Unlock()
		}

		// Проверяем также вариант без года
		url = fmt.Sprintf("https://telegra.ph/%s-%s-%s", query, month, day)
		if article, err := FindArticle(client, url); err == nil && article != "" {
			mu.Lock()
			results = append(results, article)
			mu.Unlock()
		}

		return nil
	})

	// Проверяем статьи с индексами от 2 до 20
	for i := 2; i <= 20; i++ {
		index := i // Создаем локальную переменную для горутины
		g.Go(func() error {
			time.Sleep(time.Duration(index-1) * 100 * time.Millisecond) // Задержка для предотвращения блокировки

			// С годом
			url := fmt.Sprintf("https://telegra.ph/%s-%s-%s-%s-%d", query, month, day, yearStr, index)
			if article, err := FindArticle(client, url); err == nil && article != "" {
				mu.Lock()
				results = append(results, article)
				mu.Unlock()
			}

			// Без года
			url = fmt.Sprintf("https://telegra.ph/%s-%s-%s-%d", query, month, day, index)
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

// FindArticlesForMonth ищет статьи за указанный месяц и год
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

// FindArticles ищет все статьи по запросу за весь год
func FindArticles(ctx context.Context, query string, progressCallback func(int, int)) ([]string, error) {
	translitQuery := Translit(query)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var results []string
	var mu sync.Mutex
	g := errgroup.Group{}

	// Проверяем каждый месяц текущего года
	currentYear := time.Now().Year()

	// Проверяем каждый месяц
	for month := 1; month <= 12; month++ {
		monthNum := month // Создаем локальную копию для горутины
		g.Go(func() error {
			// Вызываем коллбэк для обновления прогресса
			if progressCallback != nil {
				progressCallback(monthNum, 12)
			}

			articles, err := FindArticlesForMonth(ctx, client, translitQuery, monthNum, currentYear)
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

// FindArticlesForSpecificURL проверяет конкретную ссылку на Telegraph
func FindArticlesForSpecificURL(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return FindArticle(client, url)
}
