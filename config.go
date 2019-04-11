package main

import (
	"github.com/ghodss/yaml"
	"io/ioutil"
)

type MetadataConfig struct {
	Pod                   map[string]MetadataSpec `json:"pod"`
	Service               map[string]MetadataSpec `json:"service"`
	PersistentVolumeClaim map[string]MetadataSpec `json:"persistentVolumeClaim"`
}

type MetadataSpec struct {
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels"`
}

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
