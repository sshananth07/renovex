// Command demoseed is a development-only tool that provisions and tears
// down a demo tenant (a separate User + Company from any real developer
// account) populated with realistic synthetic renovation-contractor data,
// for product/project-progress demonstrations. It refuses to run outside an
// explicitly-declared development environment — see internal/demoseed.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demoseed:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || (os.Args[1] != "seed" && os.Args[1] != "reset") {
		return fmt.Errorf("usage: demoseed <seed|reset>")
	}
	subcommand := os.Args[1]

	// The environment guard reads the PROCESS environment directly via
	// os.LookupEnv — it runs BEFORE any .env loading, so a .env file
	// setting APP_ENV=development/RENOVEX_DEMO_TOOL_ENABLED=true CANNOT
	// satisfy this check on its own. Both variables must be exported into
	// the actual shell/process environment the CLI runs in.
	if err := demoseed.CheckEnvironmentGuard(); err != nil {
		return err
	}

	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../.env")
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	demoPassword := os.Getenv("RENOVEX_DEMO_PASSWORD")
	if subcommand == "seed" && demoPassword == "" {
		return fmt.Errorf("RENOVEX_DEMO_PASSWORD must be set to seed the demo tenant")
	}

	logger := logging.New(os.Stdout, "info")

	mongoClient, err := platformmongo.Connect(cfg.MongoURI)
	if err != nil {
		return fmt.Errorf("connect to mongodb: %w", err)
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), mongoClient)
	}()
	db := platformmongo.Database(mongoClient, cfg.MongoDatabase)

	ctx := context.Background()
	services, err := composition.BuildServices(ctx, cfg, logger, db)
	if err != nil {
		return fmt.Errorf("build service graph: %w", err)
	}

	if err := demoseed.CheckMailerIsLocalSink(cfg); err != nil {
		return err
	}

	switch subcommand {
	case "seed":
		result, err := demoseed.Seed(ctx, services, db, demoPassword)
		if err != nil {
			return err
		}
		fmt.Printf("demo tenant ready: company=%q companyId=%s demoEmail=%s\n",
			result.CompanyName, result.CompanyID, result.DemoEmail)
		return nil
	case "reset":
		result, err := demoseed.Reset(ctx, services, db)
		if err != nil {
			return err
		}
		if result.WasNoOp {
			fmt.Println("demo tenant already reset (no-op)")
		} else {
			fmt.Printf("demo tenant reset: companyId=%s\n", result.CompanyID)
		}
		return nil
	}
	return nil // unreachable — subcommand validated above
}
