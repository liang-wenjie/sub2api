package pluginregistry

type FrontendMode string

const (
	FrontendModeHosted FrontendMode = "hosted"
	FrontendModeRemote FrontendMode = "remote"
)

type Metadata struct {
	Key              string
	Name             string
	Description      string
	Enabled          bool
	FrontendMode     FrontendMode
	DefaultEntryPath string
	RemoteEntryURL   string
}

type Plugin interface {
	Metadata() Metadata
}

type StaticPlugin struct {
	Meta Metadata
}

func (p StaticPlugin) Metadata() Metadata {
	return p.Meta
}
