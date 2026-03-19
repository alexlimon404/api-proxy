package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config хранит настройки приложения, загруженные из .env файла.
type Config struct {
	TargetURL   string        // URL целевого сервера, к которому проксируются запросы
	CacheTTL    time.Duration // время жизни записи в кеше в секундах (например 3600)
	Port        string        // порт, на котором слушает прокси-сервер
	CacheRoutes []string      // список путей, которые разрешено кешировать (пустой = кешировать всё)
}

// Load читает .env файл и возвращает заполненную конфигурацию.
// Если TARGET_URL не задан — возвращает ошибку, так как без него прокси бесполезен.
// CACHE_TTL по умолчанию 3600 секунд, PORT по умолчанию "8080".
func Load() (*Config, error) {
	// godotenv.Load() загружает переменные из .env в os.Environ.
	// Ошибку игнорируем — .env может отсутствовать, если переменные заданы иначе.
	godotenv.Load()

	// TARGET_URL — обязательный параметр: адрес сервера, куда проксируем запросы
	targetURL := os.Getenv("TARGET_URL")
	if targetURL == "" {
		return nil, fmt.Errorf("TARGET_URL is required")
	}

	// CACHE_TTL — время жизни кеша в секундах (например 3600 = 1 час)
	ttlStr := os.Getenv("CACHE_TTL")
	if ttlStr == "" {
		ttlStr = "3600"
	}
	ttlSeconds, err := strconv.Atoi(ttlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CACHE_TTL (must be seconds): %w", err)
	}
	ttl := time.Duration(ttlSeconds) * time.Second

	// PORT — порт HTTP-сервера прокси
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// CACHE_ROUTES — список путей через запятую, которые разрешено кешировать.
	// Если не задан — кешируются все GET-запросы.
	// Пример: "/api/translations/simple,/api/products"
	var cacheRoutes []string
	routesStr := os.Getenv("CACHE_ROUTES")
	if routesStr != "" {
		for _, route := range strings.Split(routesStr, ",") {
			route = strings.TrimSpace(route)
			if route != "" {
				// гарантируем, что путь начинается с /
				if !strings.HasPrefix(route, "/") {
					route = "/" + route
				}
				cacheRoutes = append(cacheRoutes, route)
			}
		}
	}

	return &Config{
		TargetURL:   targetURL,
		CacheTTL:    ttl,
		Port:        port,
		CacheRoutes: cacheRoutes,
	}, nil
}
