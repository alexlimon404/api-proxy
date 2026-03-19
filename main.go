package main

import (
	"log"
	"net/http"

	"api-proxy/cache"
	"api-proxy/config"
	"api-proxy/proxy"
)

func main() {
	// загружаем конфигурацию из .env файла
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// создаём in-memory кеш с TTL из конфигурации
	c := cache.New(cfg.CacheTTL)

	// создаём прокси-обработчик, который проксирует запросы к целевому серверу
	p, err := proxy.New(cfg.TargetURL, c, cfg.CacheRoutes)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("proxy listening on :%s → %s (cache TTL: %s)", cfg.Port, cfg.TargetURL, cfg.CacheTTL)

	// запускаем HTTP-сервер. Proxy реализует http.Handler,
	// поэтому все входящие запросы попадают в Proxy.ServeHTTP
	if err := http.ListenAndServe(":"+cfg.Port, p); err != nil {
		log.Fatal(err)
	}
}
