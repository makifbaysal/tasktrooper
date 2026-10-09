// Command skillvectors embeds the catalog's skills once, with the desktop's
// embedder, into catalog/skills.vectors.json. Run it after a skill changes:
//
//	skillvectors -catalog ../catalog -embeddings-url http://127.0.0.1:<port>/v1
//	skillvectors -catalog ../catalog -check
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/skillvectors"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "skillvectors:", err)
		os.Exit(1)
	}
}

func run() error {
	var opts skillvectors.Options
	check := flag.Bool("check", false, "report skills without a vector and vectors without a skill; embeds nothing")
	flag.StringVar(&opts.CatalogDir, "catalog", "../catalog", "the catalog directory (holds agents/)")
	flag.StringVar(&opts.Out, "out", "", "the vectors file (default <catalog>/skills.vectors.json)")
	flag.StringVar(&opts.EmbeddingsURL, "embeddings-url", "", "the desktop embedder's OpenAI-compatible base URL")
	flag.StringVar(&opts.Source, "source", "", "the engine behind that URL (default onnx-int8)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *check {
		missing, stale, err := skillvectors.Check(ctx, opts.CatalogDir, opts.Out)
		if err != nil {
			return err
		}
		fmt.Printf("%d skills without a vector, %d vectors without a skill\n", missing, stale)
		if missing > 0 {
			return fmt.Errorf("%d skills would be embedded live; run skillvectors with -embeddings-url", missing)
		}
		return nil
	}

	opts.Log = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	report, err := skillvectors.Build(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d skills, %d distinct texts, %d embedded, %d kept, %d dropped (%s, %d dims)\n",
		report.Out, report.Skills, report.Unique, report.Embedded, report.Reused, report.Dropped, report.Model, report.Dims)
	return nil
}
