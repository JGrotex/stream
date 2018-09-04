package pipeline

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/TIBCOSoftware/flogo-lib/core/activity"
	"github.com/TIBCOSoftware/flogo-lib/core/data"
	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/flogo-oss/stream/pipeline/support"
)

type Status int

const (
	// StatusNotStarted indicates that the Pipeline has not started
	StatusNotStarted Status = 0

	// StatusActive indicates that the Pipeline is active
	StatusActive Status = 100

	// StatusDone indicates that the Pipeline is done
	StatusDone Status = 500
)

type ExecutionStatus int

const (
	// ExecStatusNotStarted indicates that the Pipeline execution has not started
	ExecStatusNotStarted ExecutionStatus = 0

	// ExecStatusActive indicates that the Pipeline execution is active
	ExecStatusActive ExecutionStatus = 100

	// ExecStatusStalled indicates that the Pipeline execution has stalled
	ExecStatusStalled ExecutionStatus = 400

	// ExecStatusCompleted indicates that the Pipeline execution has been completed
	ExecStatusCompleted ExecutionStatus = 500

	// ExecStatusCancelled indicates that the Pipeline execution has been cancelled
	ExecStatusCancelled ExecutionStatus = 600

	// ExecStatusFailed indicates that the Pipeline execution has failed
	ExecStatusFailed ExecutionStatus = 700
)

const (
	bitIsTimer  uint8 = 1
	bitIsTicker uint8 = 2
)

type ExecutionContext struct {
	pipeline      *Instance
	discriminator string

	stageId int
	status  ExecutionStatus

	pipelineInput map[string]*data.Attribute
	pipeineOutput map[string]*data.Attribute

	currentInput  map[string]*data.Attribute
	currentOutput map[string]*data.Attribute

	updateTimers uint8
}

func (eCtx *ExecutionContext) Status() ExecutionStatus {
	return eCtx.status
}

func (eCtx *ExecutionContext) currentStage() *Stage {
	//possibly keep pointer to state in ctx?
	return eCtx.pipeline.def.stages[eCtx.stageId]
}

func (eCtx *ExecutionContext) pipelineScope() data.MutableScope {
	//todo just maybe store ref to pipeline state in ctx
	return eCtx.pipeline.sm.GetState(eCtx.discriminator).GetScope()
}

/////////////////////////////////////////
//  activity.Host Implementation

func (eCtx *ExecutionContext) ID() string {
	return eCtx.pipeline.id
}

func (eCtx *ExecutionContext) Name() string {
	return eCtx.pipeline.def.name
}

func (eCtx *ExecutionContext) IOMetadata() *data.IOMetadata {
	return eCtx.pipeline.def.metadata
}

func (eCtx *ExecutionContext) Reply(replyData map[string]*data.Attribute, err error) {
	//ignore - not supported by pipeline
}

func (eCtx *ExecutionContext) Return(returnData map[string]*data.Attribute, err error) {
	//ignore - not supported by pipeline
}

func (eCtx *ExecutionContext) WorkingData() data.Scope {
	return eCtx.pipeline.sm.GetState(eCtx.discriminator).GetScope()
}

func (eCtx *ExecutionContext) GetResolver() data.Resolver {
	return data.GetBasicResolver()
}

/////////////////////////////////////////
//  activity.Context Implementation

func (eCtx *ExecutionContext) ActivityHost() activity.Host {
	return eCtx
}

func (eCtx *ExecutionContext) GetSetting(setting string) (value interface{}, exists bool) {
	stage := eCtx.currentStage()
	attr, found := stage.settings[setting]
	if found {
		return attr.Value(), true
	}

	return nil, false
}

func (eCtx *ExecutionContext) GetInput(name string) interface{} {

	attr, found := eCtx.currentInput[name]
	if found {
		return attr.Value()
	} else {
		stage := eCtx.currentStage()
		attr, found := stage.act.Metadata().Input[name]
		if found {
			return attr.Value()
		}
	}

	return nil
}

func (eCtx *ExecutionContext) GetOutput(name string) interface{} {
	attr, found := eCtx.currentOutput[name]
	if found {
		return attr.Value()
	} else {
		stage := eCtx.currentStage()
		attr, found := stage.outputAttrs[name]
		if found {
			return attr.Value()
		}
	}

	return nil
}

func (eCtx *ExecutionContext) SetOutput(name string, value interface{}) {

	if eCtx.currentOutput == nil {
		eCtx.currentOutput = make(map[string]*data.Attribute)
	}

	attr, found := eCtx.currentOutput[name]
	if found {
		attr.SetValue(value)
	} else {
		//get type from the stages output or existing metadata
		//todo
		attr, _ = data.NewAttribute(name, data.TypeAny, value)
		eCtx.currentOutput[name] = attr
	}
}

func (eCtx *ExecutionContext) GetSharedTempData() map[string]interface{} {

	state := eCtx.pipeline.sm.GetState(eCtx.discriminator)
	return state.GetSharedData(eCtx.currentStage().act)
}

// DEPRECATED
func (eCtx *ExecutionContext) GetInitValue(key string) (value interface{}, exists bool) {
	//ignore
	return nil, false
}

// DEPRECATED
func (eCtx *ExecutionContext) TaskName() string {
	//ignore
	return ""
}

// DEPRECATED
func (eCtx *ExecutionContext) FlowDetails() activity.FlowDetails {
	//ignore
	return nil
}

/////////////////////////////////////////
//  TimerSupport Implementation

// HasTimer indicates if a timer already exists
func (eCtx *ExecutionContext) HasTimer(repeating bool) bool {
	act := eCtx.currentStage().act
	eCtx.pipeline.sm.GetState(eCtx.discriminator)

	var hasTimer bool

	state := eCtx.pipeline.sm.GetState(eCtx.discriminator)

	if repeating {
		_, hasTimer = state.GetTimer(act)
	} else {
		_, hasTimer = state.GetTimer(act)
	}

	return hasTimer
}

// CancelTimer cancels the existing timer
func (eCtx *ExecutionContext) CancelTimer(repeating bool) {
	act := eCtx.currentStage().act

	state := eCtx.pipeline.sm.GetState(eCtx.discriminator)

	if repeating {
		state.RemoveTicker(act)
	} else {
		state.RemoveTimer(act)
	}
}

// UpdateTimer creates a timer, note: can only have one active timer at a time for an activity
func (eCtx *ExecutionContext) UpdateTimer(repeating bool) {

	if repeating {
		eCtx.updateTimers = eCtx.updateTimers | bitIsTicker
	} else {
		eCtx.updateTimers = eCtx.updateTimers | bitIsTimer
	}
}

// UpdateTimers creates a timer, note: can only have one active timer at a time for an activity
func (eCtx *ExecutionContext) UpdateTimers() {
	act := eCtx.currentStage().act
	state := eCtx.pipeline.sm.GetState(eCtx.discriminator)

	if eCtx.updateTimers&bitIsTicker > 0 {
		if holder, exists := state.GetTicker(act); exists {
			holder.SetLastExecCtx(eCtx)
		}
	} else if eCtx.updateTimers&bitIsTimer > 0 {
		if holder, exists := state.GetTimer(act); exists {
			holder.SetLastExecCtx(eCtx)
		}
	}
	eCtx.updateTimers = 0
}

// CreateTimer creates a timer, note: can only have one active timer at a time for an activity
func (eCtx *ExecutionContext) CreateTimer(interval time.Duration, callback support.TimerCallback, repeating bool) error {

	//todo add "clone ctx flag, incase exec context isn't discarded)
	//discriminator := eCtx.discriminator
	//inst := eCtx.pipeline
	//stageId := eCtx.stageId

	state := eCtx.pipeline.sm.GetState(eCtx.discriminator)

	if repeating {
		//create go ticker

		holder, err := state.NewTicker(eCtx.currentStage().act, interval)
		if err != nil {
			return err
		}
		//todo should this clone ctx?
		holder.SetLastExecCtx(eCtx)

		go func() {

			for range holder.ticker.C {

				newCtx := holder.GetLastExecCtx()
				//newCtx := &ExecutionContext{discriminator: discriminator, pipeline: inst}
				//newCtx.stageId = stageId
				//newCtx.status = ExecStatusActive

				//todo - what should we do if no samples have come in a window,  ignore for now

				if newCtx != nil {
					logger.Debugf("Repeating timer fired for activity: %s", newCtx.currentStage().act.Metadata().ID)

					resume := invokeCallback(callback, newCtx)
					//resume := callback(newCtx)
					if resume {
						Resume(newCtx)
					}
				} else {
					logger.Debugf("Repeating timer fired for activity: %s, but not running since no samples in window", "activity")
				}
			}
		}()

	} else {
		//create go timer

		holder, err := state.NewTimer(eCtx.currentStage().act, interval)
		if err != nil {
			return err
		}
		//todo should this clone ctx?
		holder.SetLastExecCtx(eCtx)

		go func() {
			<-holder.timer.C
			newCtx := holder.GetLastExecCtx()
			//newCtx := &ExecutionContext{discriminator: discriminator, pipeline: inst}
			//newCtx.stageId = stageId
			//newCtx.status = ExecStatusActive

			logger.Debugf("Timeout timer fired for activity: %s", newCtx.currentStage().act.Metadata().ID)

			resume := invokeCallback(callback, newCtx)
			//resume := callback(newCtx)
			if resume {
				Resume(newCtx)
			}
		}()
	}

	return nil
}


func invokeCallback(callback support.TimerCallback, ctx activity.Context) (resume bool) {

	defer func() {
		if r := recover(); r != nil {

			err := fmt.Errorf("unhandled error executing callback for stage '%s' : %v", ctx.Name(), r)
			logger.Error(err)

			// todo: useful for debugging
			logger.Debugf("StackTrace: %s", debug.Stack())

			resume = false
		}
	}()

	resume = callback(ctx)
	return resume
}