// Copyright (c) F5, Inc.
//
// This source code is licensed under the Apache License, Version 2.0 license found in the
// LICENSE file in the root directory of this source tree.

package instance

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	mpi "github.com/nginx/agent/v3/api/grpc/mpi/v1"
	"github.com/nginx/agent/v3/internal/datasource/host/exec"
	"github.com/nginx/agent/v3/pkg/id"
	"github.com/nginx/agent/v3/pkg/nginxprocess"
)

const (
	withWithPrefix   = "with-"
	withModuleSuffix = "module"
	keyValueLen      = 2
	flagLen          = 1
)

type (
	NginxProcessParser struct {
		executer exec.ExecInterface
	}

	Info struct {
		ConfigureArgs   map[string]interface{}
		Version         string
		Prefix          string
		ConfPath        string
		ExePath         string
		LoadableModules []string
		DynamicModules  []string
		ProcessID       int32
	}
)

var (
	_ processParser = (*NginxProcessParser)(nil)

	versionRegex = regexp.MustCompile(`(?P<name>\S+)\/(?P<version>.*)`)
)

func NewNginxProcessParser() *NginxProcessParser {
	return &NginxProcessParser{
		executer: &exec.Exec{},
	}
}

// cognitive complexity of 16 because of the if statements in the for loop
// don't think can be avoided due to the need for continue
// nolint: revive
func (npp *NginxProcessParser) Parse(ctx context.Context, processes []*nginxprocess.Process) map[string]*mpi.Instance {
	instanceMap := make(map[string]*mpi.Instance)   // key is instanceID
	workers := make(map[int32][]*mpi.InstanceChild) // key is ppid of process

	processesByPID := convertToMap(processes)

	for _, proc := range processesByPID {
		if proc.IsWorker() {
			// Here we are determining if the worker process has a master
			if masterProcess, ok := processesByPID[proc.PPID]; ok {
				workers[masterProcess.PID] = append(workers[masterProcess.PID],
					&mpi.InstanceChild{ProcessId: proc.PID})

				continue
			}
			nginxInfo, err := npp.info(ctx, proc)
			if err != nil {
				slog.DebugContext(ctx, "Unable to get NGINX info", "pid", proc.PID, "error", err)

				continue
			}
			// set instance process ID to 0 as there is no master process
			nginxInfo.ProcessID = 0

			instance := convertInfoToInstance(*nginxInfo)

			if foundInstance, ok := instanceMap[instance.GetInstanceMeta().GetInstanceId()]; ok {
				foundInstance.GetInstanceRuntime().InstanceChildren = append(foundInstance.GetInstanceRuntime().
					GetInstanceChildren(), &mpi.InstanceChild{ProcessId: proc.PID})

				continue
			}

			instance.GetInstanceRuntime().InstanceChildren = append(instance.GetInstanceRuntime().
				GetInstanceChildren(), &mpi.InstanceChild{ProcessId: proc.PID})

			instanceMap[instance.GetInstanceMeta().GetInstanceId()] = instance

			continue
		}

		// check if proc is a master process, process is not a worker but could be cache manager etc
		if proc.IsMaster() {
			nginxInfo, err := npp.info(ctx, proc)
			if err != nil {
				slog.DebugContext(ctx, "Unable to get NGINX info", "pid", proc.PID, "error", err)

				continue
			}

			instance := convertInfoToInstance(*nginxInfo)
			instanceMap[instance.GetInstanceMeta().GetInstanceId()] = instance
		}
	}

	for _, instance := range instanceMap {
		if val, ok := workers[instance.GetInstanceRuntime().GetProcessId()]; ok {
			instance.InstanceRuntime.InstanceChildren = val
		}
	}

	return instanceMap
}

func (npp *NginxProcessParser) info(ctx context.Context, proc *nginxprocess.Process) (*Info, error) {
	exePath := proc.Exe

	if exePath == "" {
		exePath = npp.exe(ctx)
		if exePath == "" {
			return nil, fmt.Errorf("unable to find NGINX exe for process %d", proc.PID)
		}
	}

	confPath := confPathFromCommand(proc.Cmd)

	var nginxInfo *Info

	outputBuffer, err := npp.executer.RunCmd(ctx, exePath, "-V")
	if err != nil {
		return nil, err
	}

	nginxInfo = parseNginxVersionCommandOutput(ctx, outputBuffer)

	nginxInfo.ExePath = exePath
	nginxInfo.ProcessID = proc.PID

	if nginxInfo.ConfPath = nginxConfPath(ctx, nginxInfo); confPath != "" {
		nginxInfo.ConfPath = confPath
	}

	loadableModules := loadableModules(nginxInfo)
	nginxInfo.LoadableModules = loadableModules

	nginxInfo.DynamicModules = dynamicModules(nginxInfo)

	return nginxInfo, err
}

func (npp *NginxProcessParser) exe(ctx context.Context) string {
	exePath := ""

	out, commandErr := npp.executer.RunCmd(ctx, "sh", "-c", "command -v nginx")
	if commandErr == nil {
		exePath = strings.TrimSuffix(out.String(), "\n")
	}

	if exePath == "" {
		exePath = npp.defaultToNginxCommandForProcessPath()
	}

	if strings.Contains(exePath, "(deleted)") {
		exePath = sanitizeExeDeletedPath(exePath)
	}

	return exePath
}

func (npp *NginxProcessParser) defaultToNginxCommandForProcessPath() string {
	exePath, err := npp.executer.FindExecutable("nginx")
	if err != nil {
		return ""
	}

	return exePath
}

func sanitizeExeDeletedPath(exe string) string {
	firstSpace := strings.Index(exe, "(deleted)")
	if firstSpace != -1 {
		return strings.TrimSpace(exe[0:firstSpace])
	}

	return strings.TrimSpace(exe)
}

func convertInfoToInstance(nginxInfo Info) *mpi.Instance {
	var instanceRuntime *mpi.InstanceRuntime
	nginxType := mpi.InstanceMeta_INSTANCE_TYPE_NGINX
	version := nginxInfo.Version

	if !strings.Contains(nginxInfo.Version, "plus") {
		instanceRuntime = &mpi.InstanceRuntime{
			ProcessId:  nginxInfo.ProcessID,
			BinaryPath: nginxInfo.ExePath,
			ConfigPath: nginxInfo.ConfPath,
			Details: &mpi.InstanceRuntime_NginxRuntimeInfo{
				NginxRuntimeInfo: &mpi.NGINXRuntimeInfo{
					StubStatus: &mpi.APIDetails{
						Location: "",
						Listen:   "",
					},
					AccessLogs:      []string{},
					ErrorLogs:       []string{},
					LoadableModules: nginxInfo.LoadableModules,
					DynamicModules:  nginxInfo.DynamicModules,
				},
			},
		}
	} else {
		instanceRuntime = &mpi.InstanceRuntime{
			ProcessId:  nginxInfo.ProcessID,
			BinaryPath: nginxInfo.ExePath,
			ConfigPath: nginxInfo.ConfPath,
			Details: &mpi.InstanceRuntime_NginxPlusRuntimeInfo{
				NginxPlusRuntimeInfo: &mpi.NGINXPlusRuntimeInfo{
					StubStatus: &mpi.APIDetails{
						Location: "",
						Listen:   "",
					},
					AccessLogs:      []string{},
					ErrorLogs:       []string{},
					LoadableModules: nginxInfo.LoadableModules,
					DynamicModules:  nginxInfo.DynamicModules,
					PlusApi: &mpi.APIDetails{
						Location: "",
						Listen:   "",
					},
				},
			},
		}

		nginxType = mpi.InstanceMeta_INSTANCE_TYPE_NGINX_PLUS
		version = nginxInfo.Version
	}

	return &mpi.Instance{
		InstanceMeta: &mpi.InstanceMeta{
			InstanceId:   id.Generate("%s_%s_%s", nginxInfo.ExePath, nginxInfo.ConfPath, nginxInfo.Prefix),
			InstanceType: nginxType,
			Version:      version,
		},
		InstanceRuntime: instanceRuntime,
	}
}

func parseNginxVersionCommandOutput(ctx context.Context, output *bytes.Buffer) *Info {
	nginxInfo := &Info{}

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "nginx version"):
			nginxInfo.Version = parseNginxVersion(line)
		case strings.HasPrefix(line, "configure arguments"):
			nginxInfo.ConfigureArgs = parseConfigureArguments(line)
		}
	}

	nginxInfo.Prefix = nginxPrefix(ctx, nginxInfo)

	return nginxInfo
}

func parseNginxVersion(line string) string {
	return strings.TrimPrefix(versionRegex.FindString(line), "nginx/")
}

func parseConfigureArguments(line string) map[string]interface{} {
	// need to check for empty strings
	flags := strings.Split(line[len("configure arguments:"):], " --")
	result := make(map[string]interface{})

	for _, flag := range flags {
		vals := strings.Split(flag, "=")
		if isFlag(vals) {
			result[vals[0]] = true
		} else if isKeyValueFlag(vals) {
			result[vals[0]] = vals[1]
		}
	}

	return result
}

func nginxPrefix(ctx context.Context, nginxInfo *Info) string {
	var prefix string

	if nginxInfo.ConfigureArgs["prefix"] != nil {
		var ok bool
		prefix, ok = nginxInfo.ConfigureArgs["prefix"].(string)
		if !ok {
			slog.DebugContext(ctx, "Failed to cast nginxInfo prefix to string")
		}
	} else {
		prefix = "/usr/local/nginx"
	}

	return prefix
}

func nginxConfPath(ctx context.Context, nginxInfo *Info) string {
	var confPath string

	if nginxInfo.ConfigureArgs["conf-path"] != nil {
		var ok bool
		confPath, ok = nginxInfo.ConfigureArgs["conf-path"].(string)
		if !ok {
			slog.DebugContext(ctx, "failed to cast nginxInfo conf-path to string")
		}
	} else {
		confPath = path.Join(nginxInfo.Prefix, "/conf/nginx.conf")
	}

	return confPath
}

func isFlag(vals []string) bool {
	return len(vals) == flagLen && vals[0] != ""
}

func isKeyValueFlag(vals []string) bool {
	return len(vals) == keyValueLen
}

func loadableModules(nginxInfo *Info) (modules []string) {
	var err error
	if mp, ok := nginxInfo.ConfigureArgs["modules-path"]; ok {
		modulePath, pathOK := mp.(string)
		if !pathOK {
			slog.Debug("Error parsing modules-path")
			return modules
		}
		modules, err = readDirectory(modulePath, ".so")
		if err != nil {
			slog.Debug("Error reading module dir", "dir", modulePath, "error", err)
			return modules
		}

		sort.Strings(modules)

		return modules
	}

	return modules
}

func dynamicModules(nginxInfo *Info) (modules []string) {
	configArgs := nginxInfo.ConfigureArgs
	for arg := range configArgs {
		if strings.HasPrefix(arg, withWithPrefix) && strings.HasSuffix(arg, withModuleSuffix) {
			modules = append(modules, strings.TrimPrefix(arg, withWithPrefix))
		}
	}

	sort.Strings(modules)

	return modules
}

// readDirectory returns a list of all files in the directory which match the extension
func readDirectory(dir, extension string) (files []string, err error) {
	dirInfo, err := os.ReadDir(dir)
	if err != nil {
		return files, fmt.Errorf("read directory %s, %w", dir, err)
	}

	for _, file := range dirInfo {
		files = append(files, strings.ReplaceAll(file.Name(), extension, ""))
	}

	return files, err
}

func convertToMap(processes []*nginxprocess.Process) map[int32]*nginxprocess.Process {
	processesByPID := make(map[int32]*nginxprocess.Process)

	for _, p := range processes {
		processesByPID[p.PID] = p
	}

	return processesByPID
}

func confPathFromCommand(command string) string {
	commands := strings.Split(command, " ")

	for i, command := range commands {
		if command == "-c" {
			if i < len(commands)-1 {
				return commands[i+1]
			}
		}
	}

	return ""
}
