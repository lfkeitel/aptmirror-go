package main

import (
	"flag"
	"log"
)

var (
	configFile string
	debugMode  bool
)

func init() {
	flag.StringVar(&configFile, "c", "config.toml", "Configuration file")
	flag.BoolVar(&debugMode, "debug", false, "Enabled debug mode")
}

func main() {
	flag.Parse()

	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	checkAndMakeDirPath(config.Skel)
	checkAndMakeDirPath(config.Dest)

	log.Println("Downloading repos")

	for _, repo := range config.Repo {
		if repo.Disable {
			log.Printf("Skipping %s, repo disabled\n", repo.URL)
			continue
		}

		r := newRepo(repo)
		if err := r.download(config); err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Finished downloading")
}
