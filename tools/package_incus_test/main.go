package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
)

var imagesRPM = []string{
	"fedora/40",
	"fedora/39",
	"centos/9-Stream",
}

var imagesDEB = []string{
	"debian/bullseye",
	"debian/bookworm",
	"ubuntu/noble",
	"ubuntu/jammy",
}

func main() {
	packageFile := ""
	image := ""

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "package",
				Usage:       ".deb or .rpm file for upgrade testing",
				Destination: &packageFile,
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "image",
				Usage:       "optional, run with specific image",
				Destination: &image,
			},
		},
		Action: func(*cli.Context) error {
			if _, err := os.Stat(packageFile); err != nil {
				return fmt.Errorf("unknown package file: %w", err)
			}

			if image != "" && packageFile != "" {
				fmt.Printf("test package %q on image %q\n", packageFile, image)
				return launchTests(packageFile, []string{image})
			} else if packageFile != "" {
				fmt.Printf("test package %q on all applicable images\n", packageFile)

				extension := filepath.Ext(packageFile)
				switch extension {
				case ".rpm":
					return launchTests(packageFile, imagesRPM)
				case ".deb":
					return launchTests(packageFile, imagesDEB)
				default:
					return fmt.Errorf("%s has unknown package type: %s", packageFile, extension)
				}
			}

			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func launchTests(packageFile string, images []string) error {
	for _, image := range images {
		fmt.Printf("starting test with %s\n", image)

		uuidWithHyphen := uuid.New()
		name := "telegraf-test-" + uuidWithHyphen.String()[0:8]

		err := runTest(image, name, packageFile)
		if err != nil {
			fmt.Printf("*** FAIL: %s\n", image)
			return err
		}

		fmt.Printf("*** PASS: %s\n\n", image)
	}

	fmt.Println("*** ALL TESTS PASS ***")
	return nil
}

func runTest(image, name, packageFile string) error {
	c := Container{Name: name}
	if err := c.Create(image); err != nil {
		return err
	}
	defer c.Delete()

	if err := c.Install("telegraf"); err != nil {
		return err
	}

	if err := c.CheckStatus("telegraf"); err != nil {
		return err
	}

	if err := c.UploadAndInstall(packageFile); err != nil {
		return err
	}

	return c.CheckStatus("telegraf")
}
