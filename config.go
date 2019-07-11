package main

import (
	"io/ioutil"

	"github.com/ghodss/yaml"
)

type MetadataConfig map[string]MetadataConstruct

type MetadataConstruct struct {
	Pod                   map[string]MetadataSpec `json:"pod"`
	Service               map[string]MetadataSpec `json:"service"`
	PersistentVolumeClaim map[string]MetadataSpec `json:"persistentVolumeClaim"`
}

type MetadataSpec map[string]string

func loadConfig(configFile string) (*MetadataConfig, error) {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg MetadataConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
