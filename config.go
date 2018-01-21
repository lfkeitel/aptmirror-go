package main

import (
	"errors"
	"io/ioutil"
	"os"

	"github.com/naoina/toml"
)

type config struct {
	Skel            string
	Dest            string
	DownloadWorkers uint
	debug           bool
	Repo            []repoConfig
}

type repoConfig struct {
	Archs      []string
	Proto      string
	URL        string
	Dist       string
	Components []string
	DisableGPG bool
	GPGKeyFile string
	Disable    bool
}

func loadConfig(configFile string) (conf *config, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("Unknown panic")
			}
		}
	}()

	f, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var con config
	if err := toml.Unmarshal(buf, &con); err != nil {
		return nil, err
	}

	return &con, nil
}
