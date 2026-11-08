// Command fiberdb is the OwnGo goroutine stand (P-NEW-09.1): an HTTP service
// on fiber v3 over SQLite. GET /fanout?n=N keeps N goroutines alive at once,
// so the same program shows how far the goroutine model of each build goes.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/gofiber/fiber/v3"
	_ "github.com/mattn/go-sqlite3"
)

// maxFanout is the target of stage G in PLAN.md (20 000 goroutines) with headroom.
const maxFanout = 100_000

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("fiberdb", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	path := fs.String("db", "fiberdb.sqlite", "SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := openDB(ctx, *path)
	if err != nil {
		return err
	}
	err = newApp(db).Listen(*addr, fiber.ListenConfig{GracefulContext: ctx, DisableStartupMessage: true})
	return errors.Join(err, db.Close())
}

func openDB(ctx context.Context, path string) (*sql.DB, error) {
	// WAL lets readers run beside the one writer; busy_timeout makes a writer
	// wait for the lock instead of failing with SQLITE_BUSY.
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS items (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		qty INTEGER NOT NULL
	)`)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create schema in %s: %w", path, err), db.Close())
	}
	return db, nil
}

type item struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Qty  int64  `json:"qty"`
}

func newApp(db *sql.DB) *fiber.App {
	app := fiber.New(fiber.Config{
		// The default handler writes err.Error() to the client.
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				return c.Status(e.Code).SendString(e.Message)
			}
			log.Printf("%s %s: %v", c.Method(), c.Path(), err)
			return c.Status(fiber.StatusInternalServerError).SendString("internal error")
		},
	})

	app.Post("/items", func(c fiber.Ctx) error {
		var it item
		if err := c.Bind().Body(&it); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
		}
		if it.Name == "" {
			return fiber.NewError(fiber.StatusBadRequest, "name is required")
		}
		err := db.QueryRowContext(c.Context(),
			`INSERT INTO items (name, qty) VALUES (?, ?) RETURNING id`, it.Name, it.Qty).Scan(&it.ID)
		if err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
		return c.Status(fiber.StatusCreated).JSON(it)
	})

	app.Get("/items/:id", func(c fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "id must be an integer")
		}
		it := item{ID: id}
		err = db.QueryRowContext(c.Context(), `SELECT name, qty FROM items WHERE id = ?`, id).Scan(&it.Name, &it.Qty)
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get item %d: %w", id, err)
		}
		return c.JSON(it)
	})

	app.Get("/items", func(c fiber.Ctx) error {
		after, err := queryInt(c, "after", 0)
		if err != nil {
			return err
		}
		limit, err := queryInt(c, "limit", 100)
		if err != nil {
			return err
		}
		if limit < 1 || limit > 1000 {
			return fiber.NewError(fiber.StatusBadRequest, "limit must be in [1, 1000]")
		}
		rows, err := db.QueryContext(c.Context(),
			`SELECT id, name, qty FROM items WHERE id > ? ORDER BY id LIMIT ?`, after, limit)
		if err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		defer rows.Close()
		items := make([]item, 0, limit) // fiber encodes with encoding/json, which writes a nil slice as null
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.ID, &it.Name, &it.Qty); err != nil {
				return fmt.Errorf("scan item: %w", err)
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		return c.JSON(items)
	})

	// Every goroutine waits until all n have started, so n are alive at once,
	// and only then reads count(*) through the pool.
	app.Get("/fanout", func(c fiber.Ctx) error {
		n, err := queryInt(c, "n", 0)
		if err != nil {
			return err
		}
		if n < 1 || n > maxFanout {
			return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("n must be in [1, %d]", maxFanout))
		}
		ctx := c.Context()
		var started, done sync.WaitGroup
		started.Add(int(n))
		release := make(chan struct{})
		var rows atomic.Int64
		errs := make([]error, n)
		for i := range n {
			done.Go(func() {
				started.Done()
				<-release
				var count int64
				errs[i] = db.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&count)
				rows.Add(count)
			})
		}
		started.Wait()
		peak := runtime.NumGoroutine()
		close(release)
		done.Wait()
		for _, err := range errs {
			if err != nil {
				return fmt.Errorf("fanout of %d: %w", n, err)
			}
		}
		return c.JSON(fiber.Map{"n": n, "peak": peak, "rows": rows.Load()})
	})

	return app
}

// queryInt reads an integer query parameter; an absent one is def.
func queryInt(c fiber.Ctx, key string, def int64) (int64, error) {
	s := c.Query(key)
	if s == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, key+" must be an integer")
	}
	return v, nil
}
