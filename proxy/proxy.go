package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"api-proxy/cache"
)

// Proxy — HTTP-обработчик, который проксирует запросы к целевому серверу
// и кеширует успешные GET-ответы.
type Proxy struct {
	cache       *cache.Cache           // in-memory кеш для хранения ответов
	reverse     *httputil.ReverseProxy // стандартный reverse proxy из stdlib
	cacheRoutes []string               // список путей, разрешённых для кеширования (пустой = всё)
}

// New создаёт новый прокси для указанного целевого URL.
// cacheRoutes — список путей, которые разрешено кешировать. Пустой список = кешировать все GET.
// Настраивает Director (подготовка запроса) и ModifyResponse (перехват ответа для кеширования).
func New(targetURL string, c *cache.Cache, cacheRoutes []string) (*Proxy, error) {
	// парсим целевой URL, чтобы извлечь scheme, host и path
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	// NewSingleHostReverseProxy создаёт прокси, который перенаправляет
	// все запросы на один целевой хост
	reverse := httputil.NewSingleHostReverseProxy(target)

	p := &Proxy{
		cache:       c,
		reverse:     reverse,
		cacheRoutes: cacheRoutes,
	}

	// Director — функция, которая модифицирует исходящий запрос перед отправкой.
	// Сохраняем оригинальный Director и дополняем его: выставляем Host заголовок,
	// чтобы целевой сервер принимал запрос (иначе Host останется от прокси).
	originalDirector := reverse.Director
	reverse.Director = func(req *http.Request) {
		originalDirector(req)  // применяем стандартную логику (подмена scheme, host, path)
		req.Host = target.Host // явно ставим Host — нужно для корректной маршрутизации на целевом сервере
	}

	// ModifyResponse вызывается после получения ответа от целевого сервера,
	// но до отправки клиенту. Здесь мы перехватываем ответ и сохраняем в кеш.
	reverse.ModifyResponse = p.modifyResponse

	return p, nil
}

// ServeHTTP — основной обработчик входящих запросов. Реализует интерфейс http.Handler.
// Логика:
//  1. Не-GET запросы проксируются напрямую без кеширования
//  2. GET-запросы сначала ищутся в кеше
//  3. При попадании (hit) — ответ отдаётся из кеша
//  4. При промахе (miss) — запрос проксируется, ответ кешируется в modifyResponse
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// эндпоинт статистики кеша — отдаём JSON со списком закешированных URL и использованием памяти
	if r.URL.Path == "/api/cache/stats" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p.cache.Stats())
		return
	}

	// кешируем только GET-запросы, остальные проксируем как есть
	if r.Method != http.MethodGet {
		p.reverse.ServeHTTP(w, r)
		return
	}

	// если задан список допустимых маршрутов — проверяем, разрешён ли путь для кеширования.
	// Сравнение по префиксу: маршрут "/api/translations" покроет и "/api/translations/simple".
	if !p.isCacheable(r.URL.Path) {
		p.reverse.ServeHTTP(w, r)
		return
	}

	// ключ кеша — полный URI запроса (путь + query string, например "/api/users?page=1")
	key := r.URL.RequestURI()

	// проверяем, есть ли ответ в кеше
	if entry, ok := p.cache.Get(key); ok {
		log.Printf("cache hit: %s", key)
		// восстанавливаем заголовки из кеша
		for k, vals := range entry.Headers {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		// отправляем статус-код и тело из кеша
		w.WriteHeader(entry.StatusCode)
		w.Write(entry.Body)
		return
	}

	// промах кеша — проксируем запрос к целевому серверу.
	// Передаём ключ кеша через заголовок запроса, чтобы modifyResponse
	// знал, под каким ключом сохранить ответ.
	log.Printf("cache miss: %s", key)
	r.Header.Set("X-Cache-Key", key)
	p.reverse.ServeHTTP(w, r)
}

// isCacheable проверяет, разрешён ли данный путь для кеширования.
// Если cacheRoutes пуст — кешируются все GET-запросы.
// Если задан — путь должен начинаться с одного из указанных маршрутов.
func (p *Proxy) isCacheable(path string) bool {
	if len(p.cacheRoutes) == 0 {
		return true // список пуст — кешируем всё
	}
	for _, route := range p.cacheRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}
	return false
}

// modifyResponse вызывается reverse proxy после получения ответа от целевого сервера.
// Если запрос был помечен заголовком X-Cache-Key (т.е. это GET с промахом кеша),
// и ответ успешный (2xx) — сохраняем его в кеш.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	// проверяем, нужно ли кешировать этот ответ
	key := resp.Request.Header.Get("X-Cache-Key")
	if key == "" {
		return nil // не GET или не требует кеширования
	}

	// читаем тело ответа целиком в память, чтобы сохранить в кеш.
	// После чтения оригинальный Body исчерпан, поэтому подменяем его
	// на новый Reader, чтобы reverse proxy мог отправить тело клиенту.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body)) // подменяем Body для дальнейшей отправки клиенту

	// кешируем только успешные ответы (2xx), чтобы не сохранять ошибки
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// копируем заголовки в отдельную map, чтобы кеш не зависел от оригинального ответа
		headers := make(map[string][]string)
		for k, v := range resp.Header {
			headers[k] = v
		}
		p.cache.Set(key, resp.StatusCode, headers, body)
	}

	return nil
}
