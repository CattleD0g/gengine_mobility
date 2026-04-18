package base

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/bilibili/gengine/context"
)

type ConcStatement struct {
	Assignments     []*Assignment
	FunctionCalls   []*FunctionCall
	MethodCalls     []*MethodCall
	ThreeLevelCalls []*ThreeLevelCall
}

func (cs *ConcStatement) AcceptAssignment(assignment *Assignment) error {
	cs.Assignments = append(cs.Assignments, assignment)
	return nil
}

func (cs *ConcStatement) AcceptFunctionCall(funcCall *FunctionCall) error {
	cs.FunctionCalls = append(cs.FunctionCalls, funcCall)
	return nil
}

func (cs *ConcStatement) AcceptMethodCall(methodCall *MethodCall) error {
	cs.MethodCalls = append(cs.MethodCalls, methodCall)
	return nil
}

func (cs *ConcStatement) AcceptThreeLevelCall(threeLevelCall *ThreeLevelCall) error {
	cs.ThreeLevelCalls = append(cs.ThreeLevelCalls, threeLevelCall)
	return nil
}

// safeCall runs fn under a panic recovery guard so that user-registered
// callables panicking in a conc {} block cannot take down the embedder. Any
// panic or returned error is appended to eMsg via errLock.
func safeCall(errLock *sync.Mutex, eMsg *[]string, label string, fn func() error) {
	defer func() {
		if rec := recover(); rec != nil {
			errLock.Lock()
			*eMsg = append(*eMsg, fmt.Sprintf("%s panicked: %v", label, rec))
			errLock.Unlock()
		}
	}()
	if err := fn(); err != nil {
		errLock.Lock()
		*eMsg = append(*eMsg, fmt.Sprintf("%+v", err))
		errLock.Unlock()
	}
}

func (cs *ConcStatement) Evaluate(dc *context.DataContext, Vars map[string]reflect.Value) (reflect.Value, error) {

	aLen := len(cs.Assignments)
	fLen := len(cs.FunctionCalls)
	mLen := len(cs.MethodCalls)
	tLen := len(cs.ThreeLevelCalls)
	l := aLen + fLen + mLen + tLen
	if l <= 0 {
		return reflect.ValueOf(nil), nil
	}

	var errLock sync.Mutex
	var eMsg []string

	var wg sync.WaitGroup
	wg.Add(l)

	for _, assign := range cs.Assignments {
		assignment := assign
		go func() {
			defer wg.Done()
			safeCall(&errLock, &eMsg, "assignment", func() error {
				_, e := assignment.Evaluate(dc, Vars)
				return e
			})
		}()
	}

	for _, fu := range cs.FunctionCalls {
		fun := fu
		go func() {
			defer wg.Done()
			safeCall(&errLock, &eMsg, "functionCall", func() error {
				_, e := fun.Evaluate(dc, Vars)
				return e
			})
		}()
	}

	for _, me := range cs.MethodCalls {
		meth := me
		go func() {
			defer wg.Done()
			safeCall(&errLock, &eMsg, "methodCall", func() error {
				_, e := meth.Evaluate(dc, Vars)
				return e
			})
		}()
	}

	for _, c := range cs.ThreeLevelCalls {
		tlc := c
		go func() {
			defer wg.Done()
			safeCall(&errLock, &eMsg, "threeLevelCall", func() error {
				_, e := tlc.Evaluate(dc, Vars)
				return e
			})
		}()
	}

	wg.Wait()

	if len(eMsg) > 0 {
		return reflect.ValueOf(nil), errors.New(fmt.Sprintf("%+v", eMsg))
	}
	return reflect.ValueOf(nil), nil
}
