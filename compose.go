package testcontainers

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/google/uuid"

	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	envProjectName = "COMPOSE_PROJECT_NAME"
	envComposeFile = "COMPOSE_FILE"
)

var ErrNoStackConfigured = errors.New("no stack files configured")

type composeStackOptions struct {
	Identifier string
	Paths      []string
}

type ComposeStackOption interface {
	applyToComposeStack(o *composeStackOptions)
}

type stackUpOptions struct {
	api.CreateOptions
	api.StartOptions
}

type StackUpOption interface {
	applyToStackUp(o *stackUpOptions)
}

type stackDownOptions struct {
	api.DownOptions
}

type StackDownOption interface {
	applyToStackDown(do *stackDownOptions)
}

// ComposeStack defines operations that can be applied to a parsed compose stack
type ComposeStack interface {
	Up(ctx context.Context, opts ...StackUpOption) error
	Down(ctx context.Context, opts ...StackDownOption) error
	Services() []string
	WaitForService(s string, strategy wait.Strategy) ComposeStack
	WithEnv(m map[string]string) ComposeStack
	WithOsEnv() ComposeStack
	ServiceContainer(ctx context.Context, svcName string) (*DockerContainer, error)
}

// DockerCompose defines the contract for running Docker Compose
// Deprecated: DockerCompose is the old shell escape based API
// use ComposeStack instead
type DockerCompose interface {
	Down() ExecError
	Invoke() ExecError
	WaitForService(string, wait.Strategy) DockerCompose
	WithCommand([]string) DockerCompose
	WithEnv(map[string]string) DockerCompose
	WithExposedService(string, int, wait.Strategy) DockerCompose
}

type waitService struct {
	service       string
	publishedPort int
}

func WithStackFiles(filePaths ...string) ComposeStackOption {
	return ComposeStackFiles(filePaths)
}

func NewDockerCompose(filePaths ...string) (*dockerCompose, error) {
	return NewDockerComposeAPIWith(WithStackFiles(filePaths...))
}

func NewDockerComposeAPIWith(opts ...ComposeStackOption) (*dockerCompose, error) {
	composeOptions := composeStackOptions{
		Identifier: uuid.New().String(),
	}

	for i := range opts {
		opts[i].applyToComposeStack(&composeOptions)
	}

	if len(composeOptions.Paths) < 1 {
		return nil, ErrNoStackConfigured
	}

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}

	if err = dockerCli.Initialize(&flags.ClientOptions{
		Common: new(flags.CommonOptions),
	}, command.WithInitializeClient(makeClient)); err != nil {
		return nil, err
	}

	composeAPI := &dockerCompose{
		name:           composeOptions.Identifier,
		configs:        composeOptions.Paths,
		composeService: compose.NewComposeService(dockerCli),
		dockerClient:   dockerCli.Client(),
		waitStrategies: make(map[string]wait.Strategy),
		containers:     make(map[string]*DockerContainer),
	}

	return composeAPI, nil
}

// NewLocalDockerCompose returns an instance of the local Docker Compose, using an
// array of Docker Compose file paths and an identifier for the Compose execution.
//
// It will iterate through the array adding '-f compose-file-path' flags to the local
// Docker Compose execution. The identifier represents the name of the execution,
// which will define the name of the underlying Docker network and the name of the
// running Compose services.
//
// Deprecated: NewLocalDockerCompose returns a DockerCompose compatible instance which is superseded
// by ComposeStack use NewDockerCompose instead to get a ComposeStack compatible instance
func NewLocalDockerCompose(filePaths []string, identifier string, opts ...LocalDockerComposeOption) *LocalDockerCompose {
	dc := &LocalDockerCompose{
		LocalDockerComposeOptions: &LocalDockerComposeOptions{
			Logger: Logger,
		},
	}

	for idx := range opts {
		opts[idx].ApplyToLocalCompose(dc.LocalDockerComposeOptions)
	}

	dc.Executable = "docker-compose"
	if runtime.GOOS == "windows" {
		dc.Executable = "docker-compose.exe"
	}

	dc.ComposeFilePaths = filePaths

	dc.absComposeFilePaths = make([]string, len(filePaths))
	for i, cfp := range dc.ComposeFilePaths {
		abs, _ := filepath.Abs(cfp)
		dc.absComposeFilePaths[i] = abs
	}

	_ = dc.determineVersion()
	_ = dc.validate()

	dc.Identifier = strings.ToLower(identifier)
	dc.waitStrategySupplied = false
	dc.WaitStrategyMap = make(map[waitService]wait.Strategy)

	return dc
}
