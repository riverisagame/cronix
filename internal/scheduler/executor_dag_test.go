package scheduler

import (
	"cronix/internal/config"
	"cronix/internal/model"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestExecutor_DAGGroupExecution_Red verifies DAG mode layered execution and blocking mechanisms.
func TestExecutor_DAGGroupExecution_Red(t *testing.T) {
	// 1. Setup in-memory DB and Executor
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 10,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 2. Create Tasks
	// Task A: returns success
	taskA := model.Task{
		Name:    "Task A",
		Command: "echo A",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskA)

	// Task B: returns success, depends on A
	taskB := model.Task{
		Name:    "Task B",
		Command: "echo B",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskB)

	// Task C: returns error (exit 1), depends on A
	taskC := model.Task{
		Name:    "Task C",
		Command: "exit 1",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskC)

	// Task D: depends on C (should not run because C fails)
	taskD := model.Task{
		Name:    "Task D",
		Command: "echo D",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskD)

	// 3. Create Dependencies
	// B -> A
	db.Create(&model.TaskDep{TaskID: taskB.ID, DependsOnID: taskA.ID})
	// C -> A
	db.Create(&model.TaskDep{TaskID: taskC.ID, DependsOnID: taskA.ID})
	// D -> C
	db.Create(&model.TaskDep{TaskID: taskD.ID, DependsOnID: taskC.ID})

	// 4. Create Group
	group := model.TaskGroup{
		Name: "DAG_Test_Group",
		Mode: "dag",
	}
	db.Create(&group)

	members := []model.Task{taskA, taskB, taskC, taskD}

	// 5. Run Group
	exec.RunGroup(&group, members, "cron")

	// 6. Assertions
	// Verify group execution log status
	var gLog model.GroupExecutionLog
	err = db.Last(&gLog).Error
	require.NoError(t, err)

	// Since C failed, group should be partial or failed
	assert.NotEqual(t, "success", gLog.Status, "Group should not succeed since Task C failed")

	// Get individual task execution logs
	var logA, logB, logC, logD model.ExecutionLog
	
	db.Where("task_id = ?", taskA.ID).First(&logA)
	db.Where("task_id = ?", taskB.ID).First(&logB)
	db.Where("task_id = ?", taskC.ID).First(&logC)
	
	// D should not exist in execution logs because it was blocked by C's failure
	errD := db.Where("task_id = ?", taskD.ID).First(&logD).Error
	assert.ErrorIs(t, errD, gorm.ErrRecordNotFound, "Task D should NOT run because its dependency Task C failed")

	// Verify timing constraint: B and C must run strictly AFTER A completes
	assert.True(t, logB.StartTime.After(*logA.EndTime) || logB.StartTime.Equal(*logA.EndTime), "Task B must wait for Task A to finish (DAG constraint)")
	assert.True(t, logC.StartTime.After(*logA.EndTime) || logC.StartTime.Equal(*logA.EndTime), "Task C must wait for Task A to finish (DAG constraint)")

	assert.NotEqual(t, "success", gLog.Status, "Group should not succeed since Task C failed")
}
