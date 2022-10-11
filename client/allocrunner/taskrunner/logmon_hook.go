package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/hashicorp/nomad/client/allocrunner/interfaces"
	"github.com/hashicorp/nomad/client/logmon"
	"github.com/hashicorp/nomad/client/pluginmanager/loggingmanager"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	bstructs "github.com/hashicorp/nomad/plugins/base/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/logging"
	pstructs "github.com/hashicorp/nomad/plugins/shared/structs"
)

const (
	// logmonReattachKey is the HookData key where logmon's reattach config
	// is stored.
	logmonReattachKey = "reattach_config"
)

// logmonHook launches logmon and manages task logging
type logmonHook struct {
	runner *TaskRunner

	// plugin is the handle to the log monitoring process for this task
	plugin *loggingmanager.LoggingPlugin

	// pluginManager dispenses plugins appropriate for the task
	pluginManager loggingmanager.Manager

	// logmon is the handle to the log monitor process for the task.
	logmon             logmon.LogMon
	logmonPluginClient *plugin.Client

	config *logmonHookConfig

	logger hclog.Logger
}

type logmonHookConfig struct {
	logDir     string
	stdoutFifo string
	stderrFifo string
}

func newLogMonHook(tr *TaskRunner, logger hclog.Logger) *logmonHook {
	hook := &logmonHook{
		runner: tr,
		config: tr.logmonHookConfig,
		logger: logger,
	}

	return hook
}

func newLogMonHookConfig(taskName, logDir string) *logmonHookConfig {
	cfg := &logmonHookConfig{
		logDir: logDir,
	}
	if runtime.GOOS == "windows" {
		id := uuid.Generate()[:8]
		cfg.stdoutFifo = fmt.Sprintf("//./pipe/%s-%s.stdout", taskName, id)
		cfg.stderrFifo = fmt.Sprintf("//./pipe/%s-%s.stderr", taskName, id)
	} else {
		cfg.stdoutFifo = filepath.Join(logDir, fmt.Sprintf(".%s.stdout.fifo", taskName))
		cfg.stderrFifo = filepath.Join(logDir, fmt.Sprintf(".%s.stderr.fifo", taskName))
	}
	return cfg
}

func (*logmonHook) Name() string {
	return "logmon"
}

func (h *logmonHook) launchLoggingPlugin(reattachConfig *plugin.ReattachConfig) error {
	plugin, err := h.pluginManager.Dispense("logmon", h.logger, reattachConfig)
	if err != nil {
		return err
	}
	h.plugin = plugin

	// l, c, err := logmon.LaunchLogMon(h.logger, reattachConfig)
	// if err != nil {
	// 	return err
	// }

	// h.logmon = l
	// h.logmonPluginClient = c
	return nil
}

func reattachConfigFromHookData(data map[string]string) (*plugin.ReattachConfig, error) {
	if data == nil || data[logmonReattachKey] == "" {
		return nil, nil
	}

	var cfg pstructs.ReattachConfig
	err := json.Unmarshal([]byte(data[logmonReattachKey]), &cfg)
	if err != nil {
		return nil, err
	}

	return pstructs.ReattachConfigToGoPlugin(&cfg)
}

func (h *logmonHook) Prestart(ctx context.Context,
	req *interfaces.TaskPrestartRequest, resp *interfaces.TaskPrestartResponse) error {

	if h.isLoggingDisabled() {
		h.logger.Debug("logging is disabled by driver")
		return nil
	}

	attempts := 0
	for {
		err := h.prestartOneLoop(ctx, req)
		if err == bstructs.ErrPluginShutdown || grpc.Code(err) == codes.Unavailable {
			h.logger.Warn("logmon shutdown while making request", "error", err)

			if attempts > 3 {
				h.logger.Warn("logmon shutdown while making request; giving up", "attempts", attempts, "error", err)
				return err
			}

			// retry after killing process and ensure we start a new logmon process
			attempts++
			h.logger.Warn("logmon shutdown while making request; retrying", "attempts", attempts, "error", err)
			h.plugin.Stop()
			//h.logmonPluginClient.Kill()
			time.Sleep(1 * time.Second)
			continue
		} else if err != nil {
			return err
		}

		rCfg := pstructs.ReattachConfigFromGoPlugin(h.plugin.ReattachConfig())
		// rCfg := pstructs.ReattachConfigFromGoPlugin(h.logmonPluginClient.ReattachConfig())
		jsonCfg, err := json.Marshal(rCfg)
		if err != nil {
			return err
		}
		resp.State = map[string]string{logmonReattachKey: string(jsonCfg)}
		return nil
	}
}

func (h *logmonHook) isLoggingDisabled() bool {
	ic, ok := h.runner.driver.(drivers.InternalCapabilitiesDriver)
	if !ok {
		return false
	}

	caps := ic.InternalCapabilities()
	return caps.DisableLogCollection
}

func (h *logmonHook) prestartOneLoop(ctx context.Context, req *interfaces.TaskPrestartRequest) error {
	// attach to a running logmon if state indicates one
	if h.plugin == nil {
		//if h.logmonPluginClient == nil {
		reattachConfig, err := reattachConfigFromHookData(req.PreviousState)
		if err != nil {
			h.logger.Error("failed to load reattach config", "error", err)
			return err
		}
		if reattachConfig != nil {
			if err := h.launchLoggingPlugin(reattachConfig); err != nil {
				h.logger.Warn("failed to reattach to logmon process", "error", err)
				// if we failed to launch logmon, try again below
			}
		}

	}

	// create a new client in initial starts, failed reattachment, or if we detect exits
	if h.plugin == nil || h.plugin.Exited() {
		//		if h.logmonPluginClient == nil || h.logmonPluginClient.Exited() {
		if err := h.launchLoggingPlugin(nil); err != nil {
			// Retry errors launching logmon as logmon may have crashed on start and
			// subsequent attempts will start a new one.
			h.logger.Error("failed to launch logmon process", "error", err)
			return structs.NewRecoverableError(err, true)
		}
	}

	err := h.plugin.Start(&logging.LogConfig{
		LogDir:        h.config.logDir,
		StdoutLogFile: fmt.Sprintf("%s.stdout", req.Task.Name),
		StderrLogFile: fmt.Sprintf("%s.stderr", req.Task.Name),
		StdoutFifo:    h.config.stdoutFifo,
		StderrFifo:    h.config.stderrFifo,
		MaxFiles:      req.Task.LogConfig.MaxFiles,
		MaxFileSizeMB: req.Task.LogConfig.MaxFileSizeMB,
	})
	if err != nil {
		h.logger.Error("failed to start logging plugin", "error", err)
		return err
	}

	err = h.logmon.Start(&logmon.LogConfig{
		LogDir:        h.config.logDir,
		StdoutLogFile: fmt.Sprintf("%s.stdout", req.Task.Name),
		StderrLogFile: fmt.Sprintf("%s.stderr", req.Task.Name),
		StdoutFifo:    h.config.stdoutFifo,
		StderrFifo:    h.config.stderrFifo,
		MaxFiles:      req.Task.LogConfig.MaxFiles,
		MaxFileSizeMB: req.Task.LogConfig.MaxFileSizeMB,
	})
	if err != nil {
		h.logger.Error("failed to start logmon", "error", err)
		return err
	}

	return nil
}

func (h *logmonHook) Stop(_ context.Context, req *interfaces.TaskStopRequest, _ *interfaces.TaskStopResponse) error {

	// // It's possible that Stop was called without calling Prestart on agent
	// // restarts. Attempt to reattach to an existing logmon.
	// if h.logmon == nil || h.logmonPluginClient == nil {
	// 	if err := h.reattach(req); err != nil {
	// 		h.logger.Trace("error reattaching to logmon when stopping", "error", err)
	// 	}
	// }

	// It's possible that Stop was called without calling Prestart on agent
	// restarts. Attempt to reattach to an existing plugin
	if h.plugin == nil {
		if err := h.reattach(req); err != nil {
			h.logger.Trace("error reattaching to plugin when stopping", "error", err)
		}
	}

	// if h.logmon != nil {
	// 	h.logmon.Stop()
	// }
	// if h.logmonPluginClient != nil {
	// 	h.logmonPluginClient.Kill()
	// }

	if h.plugin != nil {
		h.plugin.Stop()
		h.plugin.Kill()
	}

	return nil
}

// reattach to a running logmon if possible. Will not start a new logmon.
func (h *logmonHook) reattach(req *interfaces.TaskStopRequest) error {
	reattachConfig, err := reattachConfigFromHookData(req.ExistingState)
	if err != nil {
		return err
	}

	// Give up if there's no reattach config
	if reattachConfig == nil {
		return nil
	}

	return h.launchLoggingPlugin(reattachConfig)
}
