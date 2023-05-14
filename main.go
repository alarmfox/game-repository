package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	mw "github.com/go-openapi/runtime/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Configuration struct {
	PostgresUrl     string        `json:"postgresUrl"`
	ListenAddress   string        `json:"listenAddress"`
	ApiPrefix       string        `json:"apiPrefix"`
	DataDir         string        `json:"dataDir"`
	EnableSwagger   bool          `json:"enableSwagger"`
	CleanupInterval time.Duration `json:"cleanupInterval"`
}

//go:embed postman
var postmanDir embed.FS

func main() {
	var (
		configPath = flag.String("config", "config.json", "Path for configuration")
		ctx        = context.Background()
	)
	flag.Parse()
	rand.Seed(time.Now().Unix())

	fcontent, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	var configuration Configuration
	if err := json.Unmarshal(fcontent, &configuration); err != nil {
		log.Fatal(err)
	}

	makeDefaults(&configuration)

	ctx, canc := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer canc()

	if err := run(ctx, configuration); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, c Configuration) error {

	db, err := gorm.Open(postgres.Open(c.PostgresUrl), &gorm.Config{
		SkipDefaultTransaction: true,
		TranslateError:         true,
	})

	if err != nil {
		return err
	}

	err = db.AutoMigrate(
		&GameModel{},
		&RoundModel{},
		&PlayerModel{},
		&TurnModel{},
		&MetadataModel{},
		&PlayerGameModel{},
		&RobotModel{})

	if err != nil {
		return err
	}

	if err := os.Mkdir(c.DataDir, os.ModePerm); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	r := chi.NewRouter()

	// basic cors
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Accept", "Authorization"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	if c.EnableSwagger {
		r.Group(func(r chi.Router) {
			opts := mw.SwaggerUIOpts{SpecURL: "/public/postman/schemas/index.yaml"}
			sh := mw.SwaggerUI(opts, nil)
			r.Handle("/docs", sh)
			r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/docs", http.StatusMovedPermanently)
			})
		})
	}

	// serving Postman directory for documentation files
	fs := http.FileServer(http.FS(postmanDir))
	r.Mount("/public/", http.StripPrefix("/public/", fs))

	// metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger)
		r.Use(middleware.Recoverer)

		var (

			// game endpoint
			gameRepository = NewGameRepository(db)
			gameController = NewGameController(gameRepository)

			// round endpoint
			roundRepository = NewRoundStorage(db)
			roundController = NewRoundController(roundRepository)

			// turn endpoint
			turnRepository = NewTurnRepository(db, c.DataDir)
			turnController = NewTurnController(turnRepository)

			// robot endpoint
			robotRepository = NewRobotStorage(db)
			robotController = NewRobotController(robotRepository)
		)

		r.Mount(c.ApiPrefix, setupRoutes(
			gameController,
			roundController,
			turnController,
			robotController,
		))
	})
	log.Printf("listening on %s", c.ListenAddress)
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return startHttpServer(ctx, r, c.ListenAddress)
	})

	g.Go(func() error {
		ticker := time.NewTicker(c.CleanupInterval)
		for {
			select {
			case <-ticker.C:
				_, err := cleanup(db)
				if err != nil {
					log.Print(err)
				}
			case <-ctx.Done():
				return nil
			}
		}
	})

	return g.Wait()

}

func startHttpServer(ctx context.Context, r chi.Router, addr string) error {
	server := http.Server{
		Addr:              addr,
		Handler:           r,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1024 * 8,
	}

	errCh := make(chan error)
	defer close(errCh)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	ctx, canc := context.WithTimeout(context.Background(), time.Second*10)
	defer canc()

	return server.Shutdown(ctx)
}

func cleanup(db *gorm.DB) (int64, error) {
	var (
		metadata []MetadataModel
		err      error
		n        int64
	)

	err = db.Transaction(func(tx *gorm.DB) error {
		err := tx.
			Where("turn_id IS NULL").
			Find(&metadata).
			Count(&n).
			Error

		if err != nil {
			return err
		}

		var deleted []int64
		for _, m := range metadata {
			if err := os.Remove(m.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Print(err)
			} else {
				deleted = append(deleted, m.ID)
			}
		}

		return tx.Delete(&[]MetadataModel{}, deleted).Error
	})

	return n, err
}

func makeDefaults(c *Configuration) {
	if c.ApiPrefix == "" {
		c.ApiPrefix = "/"
	}

	if c.ListenAddress == "" {
		c.ListenAddress = "localhost:3000"
	}

	if c.DataDir == "" {
		c.DataDir = "data"
	}

	if int64(c.CleanupInterval) == 0 {
		c.CleanupInterval = time.Hour
	}

}
