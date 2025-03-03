package models

import (
	"context"
	"fmt"
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
	animationDuration = time.Millisecond * 1003
)

var (
	Service      = resource.NewModel("vijayvuyyuru", "weatherbox-service", "service")
	animationMap = map[string][]Animation{
		"sunny": {
			{Command: map[string]any{
				"0": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 70, 0}},
				},
				"1": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 120}},
				},
				"2": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 5}},
				},
			},
				Duration: animationDuration,
			},
			{Command: map[string]any{
				"0": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 5}},
				},
				"1": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 70, 0}},
				},
				"2": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 120}},
				},
			},
				Duration: animationDuration,
			},
			{Command: map[string]any{
				"0": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 5}},
				},
				"1": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 70, 0}},
				},
				"2": map[string]any{
					"set_animation": "pulse",
					"speed":         0.001,
					"period":        1,
					"colors":        []any{[]any{255, 0, 120}},
				},
			},
				Duration: animationDuration,
			},
		}}
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

func (cfg *Config) Validate(path string) ([]string, error) {
	// Add config validation code here
	if cfg.RefreshInterval == 0 {
		return nil, fmt.Errorf(`expected "refresh-interval" attribute for weather module`)
	}
	if cfg.WeatherSensor == "" {
		return nil, fmt.Errorf(`expected "weather-sensor" attribute for weather module`)
	}
	if cfg.LedComponent == "" {
		return nil, fmt.Errorf(`expected "led-component" attribute for weather module`)
	}
	return []string{cfg.WeatherSensor, cfg.LedComponent}, nil
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

	animationCtx    context.Context
	animationCancel func()
	animationWg     sync.WaitGroup
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
	conditionRaw, ok := reading["condition"]
	if !ok {
		s.logger.Error("no condition reading from weather sensor")
		return
	}
	_, ok = conditionRaw.(string)
	if !ok {
		s.logger.Error("condition reading from weather sensor is not a string")
		return
	}
	s.handleWeatherCondition(ctx, "sunny")
}

func (s *weatherboxServiceService) handleWeatherCondition(ctx context.Context, condition string) {
	// Cancel any existing animation goroutine
	if s.animationCancel != nil {
		s.animationCancel()
		s.animationWg.Wait()
	}

	animations, exists := animationMap[strings.ToLower(condition)]
	if !exists {
		s.logger.Error("no animations found for condition", "condition", condition)
		return
	}

	// Create new context for this animation sequence
	s.animationCtx, s.animationCancel = context.WithCancel(ctx)
	s.animationWg.Add(1)

	// Create a new goroutine for animation
	go func() {
		defer s.animationWg.Done()
		for {
			select {
			case <-s.animationCtx.Done():
				return
			default:
				for _, animation := range animations {
					if err := s.animationCtx.Err(); err != nil {
						return
					}

					// Execute the animation command
					if _, err := s.ledComponent.DoCommand(s.animationCtx, animation.Command); err != nil {
						s.logger.Error("error executing animation command", "error", err)
						continue
					}

					// Wait for the animation duration
					select {
					case <-s.animationCtx.Done():
						return
					case <-time.After(animation.Duration):
						continue
					}
				}
			}
		}
	}()
}

func (s *weatherboxServiceService) Close(context.Context) error {
	s.cancelFunc()

	if s.animationCancel != nil {
		s.animationCancel()
		s.animationWg.Wait()
	}

	if s.ledCancelFunc != nil {
		s.ledCancelFunc()
		s.ledWg.Wait()
	}
	return nil
}
