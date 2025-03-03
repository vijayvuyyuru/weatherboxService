package models

import "time"

type Animation struct {
	Command  map[string]any
	Duration time.Duration
}

type AnimationList struct {
	Animations []Animation
	index      int
}

func (a *AnimationList) NextAnimation() Animation {
	animation := a.Animations[a.index]
	a.index++
	if a.index >= len(a.Animations) {
		a.index = 0
	}
	return animation
}
