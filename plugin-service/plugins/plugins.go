package plugins

import (
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
	imagegeneration "github.com/Wei-Shaw/sub2api/plugin-service/plugins/image-generation"
)

func RegisterAll(registry *pluginregistry.Registry) error {
	return registry.Register(imagegeneration.New())
}
