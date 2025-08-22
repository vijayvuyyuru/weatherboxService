package weatherviz

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	genericComponent "go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

const (
	hot    = 65.0
	cold   = 33.0
	period = 3
)

var (
	Weatherviz       = resource.NewModel("vijayvuyyuru", "weatherviz", "weatherviz")
	errUnimplemented = errors.New("unimplemented")
	orange           = []any{255, 70, 0}
	red              = []any{255, 0, 120}
	magenta          = []any{255, 0, 5}

	purple       = []any{150, 0, 255}
	blue         = []any{0, 0, 255}
	green        = []any{50, 250, 10}
	cyan         = []any{69, 255, 226}
	white        = []any{255, 255, 255}
	animationMap = map[string]map[string]any{
		"sunny/hot":   generateSequence([][]any{orange, red, magenta}),
		"sunny/cold":  generateSequence([][]any{orange, purple, blue}),
		"cloudy/hot":  generateSequence([][]any{white, magenta, red}),
		"cloudy/cold": generateSequence([][]any{white, purple, blue}),
		"rainy/hot":   generateSequence([][]any{cyan, magenta, red}),
		"none":        generateSequence([][]any{green, magenta, red}),
		"all":         generateSequence([][]any{magenta, purple, orange}),
	}
)

func generateSequence(colors [][]any) map[string]any {
	sequences := make(map[string]any)

	// Generate three sequences with different starting points
	for i := 0; i < 3; i++ {
		animations := make([]map[string]any, len(colors))

		// Create animations array with rotated colors
		for j := 0; j < len(colors); j++ {
			colorIndex := (i + j) % len(colors)
			animations[j] = map[string]any{
				"set_animation": "pulse",
				"speed":         0.001,
				"period":        period,
				"colors":        []any{colors[colorIndex]},
			}
		}

		// Create sequence for this rotation
		sequences[strconv.Itoa(i)] = map[string]any{
			"sequence": map[string]any{
				"animations": animations,
				"duration":   period,
			},
		}
	}

	return sequences
}

func init() {
	resource.RegisterService(generic.API, Weatherviz,
		resource.Registration[resource.Resource, *Config]{
			Constructor: newWeathervizWeatherviz,
		},
	)
}

type Config struct {
	LedComponent  string `json:"led-component"`
	WeatherSensor string `json:"weather-sensor"`
}

// Validate ensures all parts of the config are valid and important fields exist.
// Returns implicit required (first return) and optional (second return) dependencies based on the config.
// The path is the JSON path in your robot's config (not the `Config` struct) to the
// resource being validated; e.g. "components.0".
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.LedComponent == "" {
		return nil, nil, fmt.Errorf(`expected "led-component" attribute for weatherviz module`)
	}
	if cfg.WeatherSensor == "" {
		return nil, nil, fmt.Errorf(`expected "weather-sensor" attribute for weatherviz module`)
	}
	return []string{cfg.LedComponent, cfg.WeatherSensor}, nil, nil
}

type weathervizWeatherviz struct {
	resource.AlwaysRebuild

	name resource.Name

	logger logging.Logger
	cfg    *Config

	weatherSensor sensor.Sensor
	ledComponent  resource.Resource

	cancelCtx  context.Context
	cancelFunc func()
}

func newWeathervizWeatherviz(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}

	return NewWeatherviz(ctx, deps, rawConf.ResourceName(), conf, logger)

}

func NewWeatherviz(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *Config, logger logging.Logger) (resource.Resource, error) {

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	weatherSensor, err := sensor.FromDependencies(deps, conf.WeatherSensor)
	if err != nil {
		cancelFunc()
		return nil, err
	}
	ledComponent, err := genericComponent.FromDependencies(deps, conf.LedComponent)
	if err != nil {
		cancelFunc()
		return nil, err
	}

	s := &weathervizWeatherviz{
		name:          name,
		logger:        logger,
		cfg:           conf,
		cancelCtx:     cancelCtx,
		cancelFunc:    cancelFunc,
		weatherSensor: weatherSensor,
		ledComponent:  ledComponent,
	}
	return s, nil
}

func (s *weathervizWeatherviz) Name() resource.Name {
	return s.name
}

// format is {visualize}

func (s *weathervizWeatherviz) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	_, ok := cmd["visualize"]
	if !ok {
		return nil, errors.New("invalid command")
	}
	return nil, s.visualize(ctx)
}

func (s *weathervizWeatherviz) visualize(ctx context.Context) error {
	readings, err := s.weatherSensor.Readings(ctx, nil)
	if err != nil {
		return err
	}
	s.logger.Infof("readings: %v", readings)
	_, ok := readings["code"].(float64)
	if !ok {
		return errors.New("code not found")
	}
	// weatherCode := 1000
	animationType := "sunny/hot"

	animation, ok := animationMap[animationType]
	if !ok {
		return errors.New("animation not found")
	}

	s.logger.Infof("visualizing weather: %v", readings)
	s.logger.Infof("animation: %v", animation)
	_, err = s.ledComponent.DoCommand(ctx, animation)
	return err
}

func (s *weathervizWeatherviz) Close(context.Context) error {
	// Put close code here
	s.cancelFunc()
	return nil
}
