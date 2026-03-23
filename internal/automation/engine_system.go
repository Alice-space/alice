package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/logging"
	"github.com/go-co-op/gocron/v2"
)

func (e *Engine) RegisterSystemTask(name string, interval time.Duration, run SystemTaskFunc) error {
	if e == nil {
		return errors.New("engine is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("system task name is empty")
	}
	if interval <= 0 {
		return errors.New("system task interval must be > 0")
	}
	if run == nil {
		return errors.New("system task function is nil")
	}

	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	if _, exists := e.systemTasks[name]; exists {
		return errors.New("system task already exists")
	}
	e.systemTasks[name] = &systemTaskRuntime{
		name:     name,
		interval: interval,
		run:      run,
	}
	return nil
}

func (e *Engine) startSystemScheduler(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.schedulerMu.Lock()
	if e.scheduler != nil {
		e.schedulerMu.Unlock()
		return nil
	}
	e.schedulerMu.Unlock()

	e.systemsMu.Lock()
	tasks := make([]*systemTaskRuntime, 0, len(e.systemTasks))
	for _, task := range e.systemTasks {
		if task == nil || task.run == nil {
			continue
		}
		tasks = append(tasks, task)
	}
	e.systemsMu.Unlock()
	if len(tasks) == 0 {
		return nil
	}

	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		taskName := task.name
		taskRun := task.run
		_, err := scheduler.NewJob(
			gocron.DurationJob(task.interval),
			gocron.NewTask(func() {
				if !e.markSystemTaskRunning(taskName) {
					return
				}
				defer e.finishSystemTask(taskName)
				defer func() {
					if recovered := recover(); recovered != nil {
						logging.Errorf("automation system task panic name=%s err=%v", taskName, recovered)
					}
				}()
				taskRun(ctx)
			}),
		)
		if err != nil {
			_ = scheduler.Shutdown()
			return fmt.Errorf("register system task %q failed: %w", taskName, err)
		}
	}
	scheduler.Start()

	e.schedulerMu.Lock()
	e.scheduler = scheduler
	e.schedulerMu.Unlock()
	return nil
}

func (e *Engine) stopSystemScheduler() {
	if e == nil {
		return
	}
	e.schedulerMu.Lock()
	scheduler := e.scheduler
	e.scheduler = nil
	e.schedulerMu.Unlock()
	if scheduler != nil {
		_ = scheduler.Shutdown()
	}
}

func (e *Engine) markSystemTaskRunning(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	task, ok := e.systemTasks[name]
	if !ok || task == nil || task.running {
		return false
	}
	task.running = true
	return true
}

func (e *Engine) finishSystemTask(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	task, ok := e.systemTasks[name]
	if !ok || task == nil {
		return
	}
	task.running = false
}
