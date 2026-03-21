package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	_ "github.com/winler/warden/internal/runtime/docker"
)

func newImagesCommand() *cobra.Command {
	images := &cobra.Command{
		Use:   "images",
		Short: "List cached warden images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listImages()
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Remove all cached warden images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return pruneImages()
		},
	}

	images.AddCommand(prune)
	return images
}

func listImages() error {
	hasAny := false
	for _, name := range runtime.AllRuntimes() {
		rt, err := runtime.NewRuntime(name)
		if err != nil {
			continue
		}
		images, err := rt.ListImages()
		if err != nil {
			continue
		}
		if len(images) > 0 {
			if !hasAny {
				fmt.Println("IMAGE\tSIZE\tRUNTIME")
			}
			hasAny = true
			for _, img := range images {
				fmt.Printf("%s\t\t%s\n", img.Tag, img.Runtime)
			}
		}
	}
	if !hasAny {
		fmt.Println("No cached warden images.")
	}
	return nil
}

func pruneImages() error {
	for _, name := range runtime.AllRuntimes() {
		rt, err := runtime.NewRuntime(name)
		if err != nil {
			continue
		}
		if err := rt.PruneImages(); err != nil {
			fmt.Fprintf(os.Stderr, "warden: warning: prune failed for %s: %v\n", name, err)
		}
	}
	return nil
}
