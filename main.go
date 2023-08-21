package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"slices"

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

var currentSessions sessionsLock

func main() {
	app := fiber.New()

	app.Use(recover.New())
	app.Use(cors.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Send(nil)
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

			// listen to signal to close and unregister (doesn't seem to work)
			go func() {
				<-notify
				log.Printf("Stopped Request\n")
				keepAliveTickler.Stop()
			}()

			for loop := true; loop; {
				select {

				case ev := <-stateChan:
					var buf bytes.Buffer
					enc := json.NewEncoder(&buf)

					m := make(map[string]any)
					m["type"] = "states"
					m["infos"] = ev

					enc.Encode(m)
					fmt.Fprintf(w, "data: %s\n\n", buf.String())
					// fmt.Printf("data: %v\n", buf.String())
					err := w.Flush()
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

	ticker := time.NewTicker(10 * time.Second)
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				fmt.Println("Ticker stopped")
				return
			case <-ticker.C:
				// fmt.Println("Tick at", t)
				wg := &sync.WaitGroup{}

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

	err := app.Listen(":8080")
	if err != nil {
		log.Panic(err)
	}

	time.Sleep(3600 * time.Millisecond)
	ticker.Stop()
	done <- true
}
