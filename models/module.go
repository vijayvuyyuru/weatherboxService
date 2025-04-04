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
	period            = 3
	duration          = period
	animationDuration = time.Millisecond*1000*period + 10

	hot  = 65.0
	cold = 33.0
)

var (
	SunnyOrange  = []any{255, 70, 0}
	SunnyRed     = []any{255, 0, 120}
	SunnyMagenta = []any{255, 0, 5}
)

var (
	Service      = resource.NewModel("vijayvuyyuru", "weatherbox-service", "service")
	animationMap = map[string]map[string]any{
		"sunny/hot": {
			"0": map[string]any{
				"sequence": map[string]any{
					"animations": []map[string]any{
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyOrange},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyRed},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyMagenta},
						},
					},
					"duration": duration,
				},
			},
			"1": map[string]any{
				"sequence": map[string]any{
					"animations": []map[string]any{
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyRed},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyMagenta},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyOrange},
						},
					},
					"duration": duration,
				},
			},
			"2": map[string]any{
				"sequence": map[string]any{
					"animations": []map[string]any{
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyMagenta},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyOrange},
						},
						{
							"animation_name": "pulse",
							"speed":          0.001,
							"period":         period,
							"colors":         []any{SunnyRed},
						},
					},
					"duration": duration,
				},
			},
		},
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
	tempString := "moderate"
	if tempOutside > hot {
		tempString = "hot"
	} else if tempOutside < cold {
		tempString = "cold"
	}
	s.handleWeatherCondition(ctx, "sunny/"+tempString)
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
