package cache

import (
	"sync"
	"time"
)

// Entry — одна запись в кеше. Хранит полный HTTP-ответ (статус, заголовки, тело)
// и момент, после которого запись считается просроченной.
type Entry struct {
	StatusCode int                 // HTTP статус-код ответа (например 200)
	Headers    map[string][]string // заголовки ответа (Content-Type и т.д.)
	Body       []byte              // тело ответа в сыром виде
	ExpiresAt  time.Time           // время, после которого запись невалидна
	Hits       int                 // сколько раз ответ был отдан из кеша
}

// Cache — потокобезопасный in-memory кеш с автоматической очисткой просроченных записей.
// Использует RWMutex: множество горутин могут читать одновременно,
// но запись блокирует всех.
type Cache struct {
	mu      sync.RWMutex      // мьютекс для потокобезопасного доступа к entries
	entries map[string]*Entry // хранилище: ключ (URI запроса) → закешированный ответ
	ttl     time.Duration     // время жизни каждой записи
}

// New создаёт новый кеш с заданным TTL и запускает фоновую горутину
// для периодической очистки просроченных записей.
func New(ttl time.Duration) *Cache {
	c := &Cache{
		entries: make(map[string]*Entry),
		ttl:     ttl,
	}
	go c.cleanup() // запускаем фоновую очистку
	return c
}

// Get возвращает запись по ключу, если она существует и ещё не просрочена.
// Второе возвращаемое значение — false, если записи нет или она истекла.
func (c *Cache) Get(key string) (*Entry, bool) {
	c.mu.Lock() // полная блокировка — при hit инкрементируем счётчик
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		// записи нет, либо TTL истёк — считаем промахом
		return nil, false
	}
	entry.Hits++ // увеличиваем счётчик попаданий в кеш
	return entry, true
}

// Set сохраняет HTTP-ответ в кеш. Время истечения вычисляется как now + TTL.
func (c *Cache) Set(key string, statusCode int, headers map[string][]string, body []byte) {
	c.mu.Lock() // полная блокировка — пишем в map
	defer c.mu.Unlock()

	c.entries[key] = &Entry{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       body,
		ExpiresAt:  time.Now().Add(c.ttl),
	}
}

// EntryStats — статистика по одной записи кеша для API ответа.
type EntryStats struct {
	URL        string `json:"url"`         // URI закешированного запроса
	StatusCode int    `json:"status_code"` // HTTP статус-код ответа
	BodySize   int    `json:"body_size"`   // размер тела ответа в байтах
	Hits       int    `json:"hits"`        // сколько раз отдан из кеша
	ExpiresAt  string `json:"expires_at"`  // время истечения в формате RFC3339
}

// Stats — общая статистика кеша для API ответа.
type Stats struct {
	TotalEntries int          `json:"total_entries"` // количество активных записей
	TotalHits    int          `json:"total_hits"`    // суммарное количество попаданий по всем записям
	TotalMemory  int          `json:"total_memory"`  // суммарный размер тел ответов в байтах
	TTL          string       `json:"ttl"`           // настроенное время жизни записей
	Entries      []EntryStats `json:"entries"`       // детали по каждой записи
}

// Stats возвращает статистику по всем активным (не просроченным) записям кеша:
// количество записей, суммарный размер в памяти и детали по каждому URL.
func (c *Cache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	var entries []EntryStats
	totalMemory := 0
	totalHits := 0

	for key, entry := range c.entries {
		// пропускаем просроченные записи — они ещё не удалены cleanup-ом
		if now.After(entry.ExpiresAt) {
			continue
		}
		bodySize := len(entry.Body)
		totalMemory += bodySize
		totalHits += entry.Hits
		entries = append(entries, EntryStats{
			URL:        key,
			StatusCode: entry.StatusCode,
			BodySize:   bodySize,
			Hits:       entry.Hits,
			ExpiresAt:  entry.ExpiresAt.Format(time.RFC3339),
		})
	}

	return Stats{
		TotalEntries: len(entries),
		TotalHits:    totalHits,
		TotalMemory:  totalMemory,
		TTL:          c.ttl.String(),
		Entries:      entries,
	}
}

// cleanup запускается в отдельной горутине и раз в минуту удаляет
// просроченные записи из кеша, чтобы не копить мусор в памяти.
func (c *Cache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.ExpiresAt) {
				delete(c.entries, key) // удаляем просроченную запись
			}
		}
		c.mu.Unlock()
	}
}
