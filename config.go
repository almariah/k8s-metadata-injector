package main

type Config struct {
  Pod                   map[string]MetadataSpec `json:"pod"`
  Service               map[string]MetadataSpec `json:"service"`
  PersistentVolumeClaim map[string]MetadataSpec `json:"persistentVolumeClaim"`
}

type MetadataSpec struct {
  Annotations map[string]string `json:"annotations"`
  Labels      map[string]string `json:"labels"`
}
