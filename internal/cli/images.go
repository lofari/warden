package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
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

func isWardenImage(tag string) bool {
	return strings.HasPrefix(tag, "warden:")
}

func listImages() error {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		fmt.Println("No cached warden images.")
		return nil
	}
	fmt.Println("IMAGE\tSIZE\tCREATED")
	fmt.Println(output)
	return nil
}

func pruneImages() error {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	images := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(images) == 0 || images[0] == "" {
		fmt.Println("No cached warden images.")
		return nil
	}
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		rmCmd := exec.Command("docker", "rmi", img)
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run()
	}
	fmt.Printf("Removed %d warden image(s).\n", len(images))
	return nil
}
