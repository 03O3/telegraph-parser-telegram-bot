import traceback

from aiogram import Bot, Dispatcher
from aiogram.types import Message
from aiogram.contrib.fsm_storage.memory import MemoryStorage
import asyncio as aio
import aiohttp
import string
from loguru import logger
import sys

bot = Bot(token="")  # Токен из @BotFather
dp = Dispatcher(bot, storage=MemoryStorage())
bot.parse_mode = 'Markdown'

logger.remove()
logger.add(sys.stderr, level="INFO")

# Сюда можно добавить ключевые слова. Если эти слова будут в статьях telegra.ph - эти статьи будут игнорироваться
ignore_list = ['https://t.me/SLlV_INTIM_BOT', 'https://t.me/+YztOEovieQIzZjY8', 'free vpn infinite time', 'mdisk', 'free exploits', 'http://openroadmdnzgrna5lzkkjlqvc662o4xbgsjqi22qjek6adq4j6emaad.onion/']

async def thread_find_current_query_url(session: aiohttp.ClientSession, query_url, save, sleep=0.1):
    logger.debug(f"CHECK {query_url}")
    await aio.sleep(sleep)
    async with session.get(query_url) as response:
        logger.debug(f"CHECK {query_url} : {response.status}")
        if response.status == 404:
            save.append(0)
            return
        else:
            resp = await response.text()
            for word in ignore_list:
                if word in resp:
                    save.append(0)
                    return
            save.append(query_url)
            return

async def create_threads_day(session, month, day, query, save_return_to):
    async with aio.TaskGroup() as tg:
        _tasks = []
        for index in range(0, 50):
            if index + 1 > 1:
                tg.create_task(thread_find_current_query_url(session,
                                                                f'https://telegra.ph/{query}-{month}-{day}-{index + 1}',
                                                                save_return_to, index / 10))
            else:
                tg.create_task(thread_find_current_query_url(session,
                                                                f'https://telegra.ph/{query}-{month}-{day}',
                                                                save_return_to))

async def create_threads_month(month, query, save_return_to):
    async with aiohttp.ClientSession() as session:
        async with aio.TaskGroup() as tg2:
            _tasks = []
            if month < 9:
                month = f'0{month + 1}'
            else:
                month = f'{month + 1}'
            for day in range(0, 30):
                if day < 9:
                    day = f'0{day + 1}'
                else:
                    day = f"{day + 1}"
                tg2.create_task(create_threads_day(session, month, day, query, save_return_to))

async def find_query(query, answer_to, message_id):
    returns = []
    async with aio.TaskGroup() as tg1:
        for month in range(0, 12):
            await bot.edit_message_text(f"В процессе: {month + 1}/12", answer_to, message_id)
            tg1.create_task(create_threads_month(month, query, returns))
        await bot.edit_message_text(f"Просматриваю ссылки и подготавливаю их...", answer_to, message_id)
    returns = [ret for ret in returns if ret]
    msgs = [returns[i:i + 50] for i in range(0, len(returns), 50)]
    logger.info(f'Спарсил {len(returns)} ссылок')
    try:
        for msg in msgs:
            await bot.send_message(answer_to, '\n'.join(msg))
    except Exception as e:
        traceback.format_exc()
    logger.info(f"Закончил парсить {query}")
    await bot.send_message(answer_to, f"Найдено {len(returns)} ссылок")



@dp.message_handler(commands=['p'])
async def start(message: Message):
    logger.info("Начинаю парсить...")
    msg_id = await message.answer("запускаю...")
    query = message.text.replace("/p ", '')
    if '/p' in query or not query:
        return "Wrong Use"
    translate_dict = {
        'а': 'a',
        'б': 'b',
        'в': 'v',
        'г': 'g',
        'д': 'd',
        'е': 'e',
        'ж': 'zh',
        'з': 'z',
        'и': 'i',
        'й': 'j',
        'к': 'k',
        'л': 'l',
        'м': 'm',
        'н': 'n',
        'о': 'o',
        'п': 'p',
        'р': 'r',
        'с': 's',
        'т': 't',
        'у': 'u',
        'ф': 'f',
        'х': 'h',
        'ц': 'ts',
        'ч': 'ch',
        'ш': 'sh',
        'щ': 'shch',
        'ъ': '',
        'ы': 'y',
        'ь': '',
        'э': 'eh',
        'ю': 'yu',
        'я': 'ya',
        ' ': '-',
        'ё': 'yo'
    }

    for k, v in translate_dict.items():
        query = query.replace(k, v)

    for char in string.punctuation.replace(' ', '').replace('-', ''):
        query = query.replace(char, '')

    aio.create_task(find_query(query, message.from_user.id, msg_id.message_id))


async def main():
    polling_task = aio.create_task(dp.start_polling())
    print("LOAD")
    while True:
        # Здесь место для функций, работающих в фоне.
        await aio.sleep(5)


loop = aio.get_event_loop()
if __name__ == '__main__':
    loop.run_until_complete(main())
    loop.close()
