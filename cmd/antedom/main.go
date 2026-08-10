// Command antedom builds or serves an antedom site: a directory
// holding pages/ (*.html page templates and *.md markdown pages —
// the files bearing ante:layout — everything else passes through),
// layout/ (layout templates named by ante:layout),
// and data/ (*.json files, in scope as data.<name>).
// "antedom build" renders the site to a directory and exits;
// "antedom serve" serves the site over HTTP, rendering per request,
// and is intended for production serving as well as development.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mrled/antedom"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var siteDir string

	root := &cobra.Command{
		Use:           "antedom",
		Short:         "Build or serve an antedom site",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&siteDir, "site", ".", "site directory holding pages/, layout/, and data/")

	var outDir string
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Render the site to a directory and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			op := antedom.NewProject(siteDir).Operation(cmd.Context(), antedom.OperationBuild)
			pages, err := op.Build(outDir)
			if err != nil {
				return err
			}
			log.Printf("built %d pages into %s", pages, outDir)
			return nil
		},
	}
	buildCmd.Flags().StringVarP(&outDir, "out", "o", "public", "output directory")

	var listen string
	var reload bool
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the site over HTTP, rendering per request",
		Long: "Serve the site over HTTP, rendering each page per request.\n" +
			"Intended for production serving as well as development.\n" +
			"Pages always render fresh; extension build outputs (sitemap.xml\n" +
			"and the like) are rebuilt on demand and cached until site files\n" +
			"change, so a served output is a snapshot of its last rebuild.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			op := antedom.NewProject(siteDir).Operation(cmd.Context(), antedom.OperationServe)
			defer op.Close()
			handler := http.Handler(op.Handler())
			if reload {
				handler = antedom.LiveReload(handler, siteDir)
			}
			server := &http.Server{Addr: listen, Handler: handler}
			go func() {
				<-cmd.Context().Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				server.Shutdown(shutdownCtx)
			}()
			log.Printf("antedom serving %s on %s", siteDir, listen)
			if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	serveCmd.Flags().StringVar(&listen, "listen", "127.0.0.1:26833", "listen address")
	// Reload defaults on for now; reconsider once serve sees production use.
	serveCmd.Flags().BoolVar(&reload, "reload", true, "reload browsers when site files change")

	var layout string
	newCmd := &cobra.Command{
		Use:   "new <path>",
		Short: "Create a new page under pages/",
		Long: "Create a new page at <path>, relative to the site's pages/ directory.\n" +
			"A path without an extension gets .md appended.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op := antedom.NewProject(siteDir).Operation(cmd.Context(), antedom.OperationNew)
			dest, err := op.NewPage(layout, args[0])
			if err != nil {
				return err
			}
			log.Printf("created %s", dest)
			return nil
		},
	}
	newCmd.Flags().StringVarP(&layout, "layout", "l", "base.html", "layout for the new page")

	root.AddCommand(buildCmd, serveCmd, newCmd)

	if err := root.ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}
