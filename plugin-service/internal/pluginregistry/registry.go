package pluginregistry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrDuplicatePluginKey = errors.New("duplicate plugin key")
	ErrInvalidPlugin      = errors.New("invalid plugin")
	ErrPluginKeyRequired  = errors.New("plugin key required")
)

type Registry struct {
	plugins map[string]Plugin
}

func New() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

func (r *Registry) Register(plugin Plugin) error {
	if plugin == nil {
		return ErrInvalidPlugin
	}

	metadata := plugin.Metadata()
	key := strings.TrimSpace(metadata.Key)
	if key == "" {
		return ErrPluginKeyRequired
	}

	metadata.Key = key
	if _, exists := r.plugins[key]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicatePluginKey, key)
	}

	r.plugins[key] = StaticPlugin{Meta: metadata}
	return nil
}

func (r *Registry) Get(key string) (Plugin, bool) {
	plugin, ok := r.plugins[key]
	return plugin, ok
}

func (r *Registry) List() []Metadata {
	keys := make([]string, 0, len(r.plugins))
	for key := range r.plugins {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	plugins := make([]Metadata, 0, len(keys))
	for _, key := range keys {
		plugins = append(plugins, r.plugins[key].Metadata())
	}
	return plugins
}
