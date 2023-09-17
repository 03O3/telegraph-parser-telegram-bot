# telegraph-parser-telegram-bot - Бот для поиска статей telegra.ph по ключевым словам

## Использование бота

`/p query` где query - ключевые слова (аккаунт, пароль, мануал и т.д.)

Механика бота заключается в том, что он ищет статьи, где ключевое слово совпадает с названием статьи

Пример: `/p аккаунт`

Бот выдаст полный список с ссылками на существующие статьи с названием "akkaunt" (Бот сам транслирует русские буквы в английские, т.к. telegraph сохраняет все названия именно на англ. языке, даже если написаны на русском)

Пример готовой ссылки на статью: https://telegra.ph/akkaunt-02-07


### Для работы бота требуется Python 3.10 версии или выше (Тестировалось и написано на 3.11)

## Установка

`git clone https://github.com/deadcxde/telegraph-parser-telegram-bot`

`cd telegraph-parser-telegram-bot`

`pip install -r requirements.txt`

## Запуск

`python main.py` (Windows)

`python3 main.py` (UNIX based)
