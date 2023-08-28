package main

import (
	"bufio"
	"bytes"
	"runtime"

	//"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/valyala/fasthttp"
)

type session struct {
	val          float64
	stateChannel chan float64
}

type sessionsLock struct {
	MU       sync.Mutex
	sessions []*session
}

func (sl *sessionsLock) addSession(s *session) {
	sl.MU.Lock()
	sl.sessions = append(sl.sessions, s)
	sl.MU.Unlock()
}

func (sl *sessionsLock) removeSession(s *session) {
	sl.MU.Lock()
	idx := slices.Index(sl.sessions, s)
	if idx != -1 {
		sl.sessions[idx] = nil
		sl.sessions = slices.Delete(sl.sessions, idx, idx+1)
	}
	sl.MU.Unlock()
}

func Filter[T any](filter func(n T) bool) func(T []T) []T {
	return func(list []T) []T {
		r := make([]T, 0, len(list))
		for _, n := range list {
			if filter(n) {
				r = append(r, n)
			}
		}
		return r
	}
}

var currentSessions sessionsLock

func formatSSEMessage(eventType string, data any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	m := map[string]any{
		"data": data,
	}

	err := enc.Encode(m)
	if err != nil {
		return "", nil
	}
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("event: %s\n", eventType))
	sb.WriteString(fmt.Sprintf("retry: %d\n", 15000))
	sb.WriteString(fmt.Sprintf("data: %v\n\n", buf.String()))

	return sb.String(), nil
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func main() {
	app := fiber.New()

	app.Use(recover.New())
	app.Use(cors.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Send(nil)
	})

	app.Get("/connections", func(c *fiber.Ctx) error {
		m := map[string]any{
			"open-connections": app.Server().GetOpenConnectionsCount(),
			"sessions":         len(currentSessions.sessions),
		}
		return c.JSON(m)
	})

	app.Get("/infos", func(c *fiber.Ctx) error {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		res := map[string]any{
			"Alloc":      bToMb(m.Alloc),
			"TotalAlloc": bToMb(m.TotalAlloc),
			"tSys":       bToMb(m.Sys),
			"tNumGC":     m.NumGC,
			"goroutines": runtime.NumGoroutine(),
		}

		// percent, _ := cpu.Percent(time.Second, true)
		// fmt.Printf("  User: %.2f\n", percent[cpu.CPUser])

		return c.JSON(res)
	})

	app.Get("/sse", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Transfer-Encoding", "chunked")

		query := c.Query("query")

		log.Printf("New Request\n")

		stateChan := make(chan float64)

		val, err := strconv.ParseFloat(query, 64)
		if err != nil {
			val = 0
		}

		s := session{
			val:          val,
			stateChannel: stateChan,
		}

		currentSessions.addSession(&s)

		notify := c.Context().Done()

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			keepAliveTickler := time.NewTicker(15 * time.Second)
			keepAliveMsg := ":keepalive\n"

			// listen to signal to close and unregister (doesn't seem to be called)
			go func() {
				<-notify
				log.Printf("Stopped Request\n")
				currentSessions.removeSession(&s)
				keepAliveTickler.Stop()
			}()

			for loop := true; loop; {
				select {

				case ev := <-stateChan:
					sseMessage, err := formatSSEMessage("current-value", ev)
					if err != nil {
						log.Printf("Error formatting sse message: %v\n", err)
						continue
					}

					// send sse formatted message
					_, err = fmt.Fprintf(w, sseMessage)

					if err != nil {
						log.Printf("Error while writing Data: %v\n", err)
						continue
					}

					err = w.Flush()
					if err != nil {
						log.Printf("Error while flushing Data: %v\n", err)
						currentSessions.removeSession(&s)
						keepAliveTickler.Stop()
						loop = false
						break
					}
				case <-keepAliveTickler.C:
					fmt.Fprintf(w, keepAliveMsg)
					err := w.Flush()
					if err != nil {
						log.Printf("Error while flushing: %v.\n", err)
						currentSessions.removeSession(&s)
						keepAliveTickler.Stop()
						loop = false
						break
					}
				}
			}

			log.Println("Exiting stream")
		}))

		return nil
	})

	// ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	// defer cancel()

	ticker := time.NewTicker(1 * time.Second)

	go func() {
		for {
			select {
			/*
				case <-ctxTimeout.Done():
					fmt.Println("Ticker stopped")
					return
			*/
			case <-ticker.C:
				// fmt.Println("Tick at", t)
				wg := &sync.WaitGroup{}

				// send a broadcast event, so all clients connected
				// will receive it, by filtering based on some info
				// stored in the session it is possible to address
				// only specific clients
				for _, s := range currentSessions.sessions {
					wg.Add(1)
					go func(cs *session) {
						defer wg.Done()
						cs.stateChannel <- cs.val + (rand.Float64() * 100)
					}(s)
				}
				wg.Wait()
			}
		}
	}()

	err := app.Listen("127.0.0.1:8080")
	if err != nil {
		log.Panic(err)
	}
}
