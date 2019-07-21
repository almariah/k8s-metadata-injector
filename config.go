package main

import (
	"github.com/ghodss/yaml"
	"io/ioutil"
)

type MetadataConfig struct {
	Namespaces map[string]NamespaceConfig `json:"namespaces"`
}

type NamespaceConfig struct {
	Pod                   MetadataSpec `json:"pod"`
	Service               MetadataSpec `json:"service"`
	PersistentVolumeClaim MetadataSpec `json:"persistentVolumeClaim"`
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

func (m *MetadataSpec) MergeMetadataSpec(added MetadataSpec) {
	for k, v := range added.Annotations {
		if _, ok := m.Annotations[k]; !ok {
			m.Annotations[k] = v
		}
	}
	for k, v := range added.Labels {
		if _, ok := m.Labels[k]; !ok {
			m.Labels[k] = v
		}
	}
}