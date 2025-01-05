# tg-svodd-bot

```
RABBIT_SERVER_URL=amqp://localhost go run .
```

```
RABBIT_SERVER_URL=amqp://localhost Q1=rabbit://q1 go run ./consumer/.
```

# development

Запускаем локально svodd-rabbitmq

http://127.0.0.1:15672/#/
guest
guest

Запускаем tg-bot

```
make up
```

или
```
docker compose up --build -d
```

Для отладки запускаем локально svodd (fct-search)

Устанавливает текущую активнцю тему, запускаем парсер
```
./app/bin/fct-parser.linux.amd64 -j -h -o ./app/data/ 
```

Запускаем Updater для записи сообщение а БД
```
docker compose run --rm cli-php php yii index/updating-index
```

