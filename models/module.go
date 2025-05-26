package models

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/pkg/errors"

	genericComponent "go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	"go.viam.com/utils/rpc"
)

const (
	period            = 3
	duration          = period
	animationDuration = time.Millisecond*1000*period + 10

	hot  = 65.0
	cold = 33.0
)

var (
	orange  = []any{255, 70, 0}
	red     = []any{255, 0, 120}
	magenta = []any{255, 0, 5}

	purple = []any{150, 0, 255}
	blue   = []any{0, 0, 255}
	green  = []any{50, 250, 10}
	cyan   = []any{69, 255, 226}
	white  = []any{255, 255, 255}
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
				"duration":   duration,
			},
		}
	}

	return sequences
}

var (
	Service      = resource.NewModel("vijayvuyyuru", "weatherbox-service", "service")
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

func init() {
	resource.RegisterService(generic.API, Service,
		resource.Registration[resource.Resource, *Config]{
			Constructor: newWeatherboxServiceService,
		},
	)
}

type Config struct {
	RefreshInterval int    `json:"refresh-interval"`
	WeatherSensor   string `json:"weather-sensor"`
	LedComponent    string `json:"led-component"`
}

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	// Add config validation code here
	if cfg.RefreshInterval == 0 {
		return nil, nil, fmt.Errorf(`expected "refresh-interval" attribute for weather module`)
	}
	if cfg.WeatherSensor == "" {
		return nil, nil, fmt.Errorf(`expected "weather-sensor" attribute for weather module`)
	}
	if cfg.LedComponent == "" {
		return nil, nil, fmt.Errorf(`expected "led-component" attribute for weather module`)
	}
	return nil, []string{cfg.WeatherSensor, cfg.LedComponent}, nil
}

type weatherboxServiceService struct {
	name resource.Name

	logger logging.Logger
	cfg    *Config

	cancelCtx  context.Context
	cancelFunc func()

	ledUpdateCtx  context.Context
	ledCancelFunc func()
	ledWg         sync.WaitGroup

	weatherSensor   sensor.Sensor
	ledComponent    resource.Resource
	refreshInterval time.Duration
}

func newWeatherboxServiceService(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	s := &weatherboxServiceService{
		name:       rawConf.ResourceName(),
		logger:     logger,
		cfg:        conf,
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
	}
	if err = s.Reconfigure(ctx, deps, rawConf); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *weatherboxServiceService) Name() resource.Name {
	return s.name
}

// outer
// [50, 255, 10], [255,0, 120], [255, 0, 5]

func (s *weatherboxServiceService) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	config, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}
	s.weatherSensor, err = sensor.FromDependencies(deps, config.WeatherSensor)
	if err != nil {
		return errors.Wrapf(err, "unable to get weather sensor %v for service", config.WeatherSensor)
	}
	s.ledComponent, err = genericComponent.FromDependencies(deps, config.LedComponent)
	if err != nil {
		return errors.Wrapf(err, "unable to get led component %v for service", config.LedComponent)
	}
	s.refreshInterval = time.Second * time.Duration(config.RefreshInterval)

	if s.ledCancelFunc != nil {
		s.ledCancelFunc()
		s.ledWg.Wait()
	}
	s.ledCancelFunc = nil
	s.ledUpdateCtx = nil
	s.ledWg = sync.WaitGroup{}

	return nil
}

func (s *weatherboxServiceService) NewClientFromConn(ctx context.Context, conn rpc.ClientConn, remoteName string, name resource.Name, logger logging.Logger) (resource.Resource, error) {
	panic("not implemented")
}

func (s *weatherboxServiceService) DoCommand(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	state, ok := cmd["state"]
	if ok {
		if state == "start" {
			if s.ledCancelFunc != nil {
				return map[string]any{"warning": "already running"}, nil
			}
			s.ledUpdateCtx, s.ledCancelFunc = context.WithCancel(s.cancelCtx)
			s.ledWg.Add(1)
			go func() {
				s.startWeatherVizService(s.ledUpdateCtx, s.refreshInterval)
			}()
			return map[string]any{"started": "true"}, nil
		} else if state == "stop" {
			if s.ledCancelFunc == nil {
				return map[string]any{"warning": "no currently running service to stop"}, nil
			}
			s.ledCancelFunc()
			s.ledWg.Wait()
			s.ledCancelFunc = nil
			s.ledUpdateCtx = nil
			return map[string]any{"stopped": "true"}, nil
		}
	}
	return map[string]any{}, nil
}

func (s *weatherboxServiceService) startWeatherVizService(ctx context.Context, interval time.Duration) {
	clk := clock.New()
	t := clk.Ticker(interval)
	defer t.Stop()
	defer s.ledWg.Done()
	s.logger.Info("starting weather visualization service")
	s.visualizeWeather(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.visualizeWeather(ctx)
		}
	}
}

func (s *weatherboxServiceService) visualizeWeather(ctx context.Context) {
	reading, err := s.weatherSensor.Readings(ctx, map[string]interface{}{})
	if err != nil {
		s.logger.Error("error reading weather sensor", "error", err)
		return
	}
	s.logger.Info("weather reading", "reading", reading)
	codeRaw, ok := reading["code"]
	if !ok {
		s.logger.Error("no condition reading from weather sensor")
		return
	}
	code, ok := codeRaw.(float64)
	if !ok {
		s.logger.Error("code reading from weather sensor is not a float")
		return
	}
	condition := getCondition(code)

	tempOutsideRaw, ok := reading["outside_f"]
	if !ok {
		s.logger.Error("no outside temperature reading from weather sensor")
		return
	}
	tempOutside, ok := tempOutsideRaw.(float64)
	if !ok {
		s.logger.Error("outside temperature reading from weather sensor is not a float")
		return
	}

	tempString := "hot"
	if tempOutside > hot {
		tempString = "cold"
	}
	fmt.Println("tempString", tempString)
	s.handleWeatherCondition(ctx, condition+"/"+tempString)
}

func getCondition(code float64) string {
	switch code {
	case 1000, 1003:
		return "sunny"
	case 1006, 1009, 1030, 1135, 1147:
		return "cloudy"
	case 1063, 1066, 1069, 1072, 1087, 1550, 1153,
		1168, 1171, 1180, 1183, 1186, 1189, 1192, 1195,
		1198, 1201, 1204, 1207, 1240, 1243, 1246, 1249,
		1252, 1273, 1276, 1279, 1282:
		return "rainy"
	}
	return "none"
}

func (s *weatherboxServiceService) handleWeatherCondition(ctx context.Context, condition string) {
	animations, exists := animationMap[strings.ToLower(condition)]
	if !exists {
		s.logger.Error("no animations found for condition", "condition", condition)
		return
	}
	_, err := s.ledComponent.DoCommand(ctx, animations)
	if err != nil {
		s.logger.Error("error setting led colors", "error", err)
		return
	}
	s.logger.Infow("led colors set for condition", "condition", condition)
}

func (s *weatherboxServiceService) Close(context.Context) error {
	s.cancelFunc()

	if s.ledCancelFunc != nil {
		s.ledCancelFunc()
		s.ledWg.Wait()
	}
	return nil
}
