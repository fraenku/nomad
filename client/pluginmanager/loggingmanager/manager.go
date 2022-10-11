package loggingmanager

import (
	"context"

	"github.com/hashicorp/go-hclog"
	log "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/hashicorp/nomad/client/pluginmanager"
	"github.com/hashicorp/nomad/helper/pluginutils/loader"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/logging"
)

// Manager is the interface implemented by manager, below
type Manager interface {
	pluginmanager.PluginManager

	Dispense(name string, logger hclog.Logger, reattachConfig *plugin.ReattachConfig) (
		*LoggingPlugin, error)
}

// manager orchestrates launching logging plugins. Note that unlike device or
// driver plugins, a logging plugin isn't monitored after launching. If the
// Nomad client exits, it becomes unparented and lives alongside the task until
// the task exits.
type manager struct {
	// logger is used by the logging plugin manager (i.e. for our own logs, not
	// for the plugins or the logs the plugins are reading)
	logger log.Logger

	// global context and cancel function for this manager
	ctx    context.Context
	cancel context.CancelFunc

	// loader is the plugin loader
	loader loader.PluginCatalog

	// pluginConfig is the config passed to the launched plugins
	pluginConfig *base.AgentConfig
}

// Config is used to configure a plugin manager
type Config struct {
	// Logger is for the plugin manager's own logs
	Logger log.Logger

	// Loader is the plugin loader
	Loader loader.PluginCatalog

	// PluginConfig is the config passed to the launched plugins
	PluginConfig *base.AgentConfig
}

// New returns a new device manager
func New(c *Config) *manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &manager{
		logger:       c.Logger.Named("logging_mgr"),
		ctx:          ctx,
		cancel:       cancel,
		loader:       c.Loader,
		pluginConfig: c.PluginConfig,
	}
}

// PluginType is the type of plugin which the manager manages
func (*manager) PluginType() string { return base.PluginTypeLogging }

// Run sets up the manager so it can dispense plugins to the logmon_hook
func (m *manager) Run() {
	plugins := m.loader.Catalog()[base.PluginTypeLogging]
	if len(plugins) == 0 {
		// TODO: is there anything to really do in this function?
		m.logger.Warn("there are no logging plugins")
		m.cancel()
		return
	}
}

// Shutdown stops any in-flight requests
func (m *manager) Shutdown() {
	m.cancel()
}

// Dispense returns a logging.LoggingPlugin for the given plugin name handling
// reattaching to an existing driver if available
func (m *manager) Dispense(pluginName string, logger hclog.Logger, reattachConfig *plugin.ReattachConfig) (*LoggingPlugin, error) {
	// TODO: the Catalog requires that we're using go-plugin style plugins, but
	// we could return anything here that implements LoggingPlugin. We'd just
	// need a new catalog to load them from.
	var instance loader.PluginInstance
	var err error
	if reattachConfig == nil {
		instance, err = m.loader.Dispense(pluginName, base.PluginTypeLogging, nil, logger)
		if err != nil {
			return nil, err
		}
	} else {
		instance, err = m.loader.Reattach(pluginName, base.PluginTypeLogging, reattachConfig)
		if err != nil {
			return nil, err
		}
	}

	// TODO: I don't like that we're returning the instance here, because that
	// ties us to the go-plugin catalog instead of being agnostic.
	p := instance.Plugin().(logging.LoggingPlugin)
	return &LoggingPlugin{p, instance}, nil
}

type LoggingPlugin struct {
	logging.LoggingPlugin
	instance loader.PluginInstance
}

func (p *LoggingPlugin) Kill() {
	p.instance.Kill()
}

func (p *LoggingPlugin) Exited() bool {
	p.instance.Exited()
}

func (p *LoggingPlugin) ReattachConfig() *plugin.ReattachConfig {
	cfg, ok := p.instance.ReattachConfig()
	if cfg == nil || !ok {
		return nil
	}
	return cfg
}
