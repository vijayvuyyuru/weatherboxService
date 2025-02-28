package models

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/pkg/errors"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils/rpc"
)

var (
	Service = resource.NewModel("vijayvuyyuru", "weatherbox-service", "service")
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
	s.ledComponent, err = resource.FromDependencies[resource.Resource](deps, resource.NewName(generic.API, config.LedComponent))
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
			s.ledUpdateCtx, s.ledCancelFunc = context.WithCancel(ctx)
			s.ledWg.Add(1)
			go func() {
				s.startWeatherVizService(s.ledUpdateCtx, s.refreshInterval)
			}()
		} else if state == "stop" {
			if s.ledCancelFunc == nil {
				return map[string]any{"warning": "no currently running service to stop"}, nil
			}
			s.ledCancelFunc()
			s.ledWg.Wait()
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
	condition, ok := conditionRaw.(string)
	if !ok {
		s.logger.Error("condition reading from weather sensor is not a string")
		return
	}
	s.handleWeatherCondition(ctx, condition)
}

func (s *weatherboxServiceService) handleWeatherCondition(ctx context.Context, condition string) {
	switch strings.ToLower(condition) {
	case "sunny":
		s.ledComponent.DoCommand(ctx, map[string]interface{}{"color": []int{255, 255, 0}})
	case "cloudy":
		s.ledComponent.DoCommand(ctx, map[string]interface{}{"color": []int{128, 128, 128}})
	}
}

func (s *weatherboxServiceService) Close(context.Context) error {
	s.cancelFunc()

	if s.ledCancelFunc != nil {
		s.ledCancelFunc()
		s.ledWg.Wait()
	}
	return nil
}
