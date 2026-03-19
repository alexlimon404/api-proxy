# API Proxy

HTTP-прокси на Go с in-memory кешированием GET-запросов. Принимает запросы от одного сервиса и проксирует их к целевому API, кешируя успешные ответы на заданное время.

## Возможности

- Проксирование всех HTTP-запросов к целевому серверу
- Кеширование успешных (2xx) GET-ответов в памяти
- Настраиваемое время жизни кеша через переменную окружения
- Автоматическая очистка просроченных записей
- Эндпоинт статистики кеша (`/api/cache/stats`)

## Требования

- Go 1.24+

## Установка

```bash
git clone <url-репозитория>
cd api-proxy
go mod tidy
```

## Настройка

Скопируйте файл с примером переменных окружения и заполните его:

```bash
cp .env.example .env
```

Откройте `.env` и укажите нужные значения:

```env
# URL целевого сервера, к которому проксируются запросы (обязательный)
TARGET_URL=https://api.example.com

# Время жизни кеша в секундах (по умолчанию 3600 = 1 час)
CACHE_TTL=3600

# Порт, на котором запускается прокси (по умолчанию 8080)
PORT=8080

# Список путей для кеширования через запятую (по умолчанию кешируются все GET)
CACHE_ROUTES=/api/translations/simple
```

| Переменная     | Обязательная | По умолчанию | Описание                                                        |
|----------------|--------------|--------------|-----------------------------------------------------------------|
| `TARGET_URL`   | да           | —            | URL целевого API                                                |
| `CACHE_TTL`    | нет          | `3600`       | Время жизни кеша в секундах                                     |
| `PORT`         | нет          | `8080`       | Порт прокси-сервера                                             |
| `CACHE_ROUTES` | нет          | — (все GET)  | Список путей для кеширования через запятую (сравнение по префиксу) |

## Запуск

### Локально

```bash
go run .
```

### Сборка и запуск бинарника

```bash
go build -o api-proxy .
./api-proxy
```

### Через Docker

Создайте `Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o api-proxy .

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/api-proxy .
CMD ["./api-proxy"]
```

Сборка и запуск:

```bash
docker build -t api-proxy .
docker run -d \
  -p 8080:8080 \
  -e TARGET_URL=https://api.example.com \
  -e CACHE_TTL=3600 \
  -e PORT=8080 \
  api-proxy
```

### Через Docker Compose

```yaml
services:
  api-proxy:
    build: .
    ports:
      - "8080:8080"
    env_file:
      - .env
    restart: unless-stopped
```

```bash
docker compose up -d
```

## Использование

После запуска прокси слушает на указанном порту. Все запросы перенаправляются к `TARGET_URL`:

```bash
# Запрос проксируется к TARGET_URL/api/users
curl http://localhost:8080/api/users

# Повторный запрос — ответ из кеша (если TTL не истёк)
curl http://localhost:8080/api/users
```

### Что кешируется

- Только `GET`-запросы
- Только успешные ответы (статус 200–299)
- Ключ кеша — полный URI (путь + query string), например `/api/users?page=1`
- POST, PUT, DELETE и другие методы проксируются без кеширования

### Статистика кеша

```bash
curl http://localhost:8080/api/cache/stats
```

Пример ответа:

```json
{
  "total_entries": 3,
  "total_memory": 15420,
  "ttl": "1h0m0s",
  "entries": [
    {
      "url": "/api/users?page=1",
      "status_code": 200,
      "body_size": 5140,
      "expires_at": "2026-03-18T15:30:00Z"
    },
    {
      "url": "/api/products",
      "status_code": 200,
      "body_size": 10280,
      "expires_at": "2026-03-18T15:45:00Z"
    }
  ]
}
```

| Поле            | Описание                                     |
|-----------------|----------------------------------------------|
| `total_entries` | Количество активных записей в кеше           |
| `total_memory`  | Суммарный размер тел ответов в байтах        |
| `ttl`           | Настроенное время жизни записей              |
| `entries`       | Массив с деталями по каждому закешированному URL |

## Структура проекта

```
api-proxy/
├── main.go            # точка входа, запуск HTTP-сервера
├── config/
│   └── config.go      # загрузка конфигурации из .env
├── cache/
│   └── cache.go       # in-memory кеш с TTL и автоочисткой
├── proxy/
│   └── proxy.go       # reverse proxy с middleware кеширования
├── .env               # переменные окружения (не в git)
├── .env.example       # пример переменных окружения
├── .gitignore
├── go.mod
└── go.sum
```

## Логирование

Прокси пишет в stdout информацию о каждом GET-запросе:

```
2026/03/18 12:00:00 proxy listening on :8080 → https://api.example.com (cache TTL: 1h0m0s)
2026/03/18 12:00:05 cache miss: /api/users
2026/03/18 12:00:06 cache hit: /api/users
```
