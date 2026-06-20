// ============================================================
// internal/database/database_test.go - database init tests
// ============================================================
package database

import (
    "path/filepath"
    "testing"
    "cronix/internal/domain/model"
)

// TestInit verifies database initialization and table creation
func TestInit(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")

    err := Init(dbPath)
    if err != nil {
        t.Fatalf("Init failed: %v", err)
    }
    // Close DB so TempDir cleanup works on Windows
    defer Close()

    if DB == nil {
        t.Fatal("DB is nil after Init")
    }

    if !DB.Migrator().HasTable(&model.Task{}) {
        t.Error("tasks table not created")
    }
    if !DB.Migrator().HasTable(&model.TaskDep{}) {
        t.Error("task_deps table not created")
    }
    if !DB.Migrator().HasTable(&model.ExecutionLog{}) {
        t.Error("execution_logs table not created")
    }
    if !DB.Migrator().HasTable(&model.NotifyConfig{}) {
        t.Error("notify_configs table not created")
    }
    t.Logf("DB init ok: %s", dbPath)
}

// TestTaskCRUD verifies basic CRUD operations on tasks
func TestTaskCRUD(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")

    if err := Init(dbPath); err != nil {
        t.Fatalf("Init failed: %v", err)
    }
    defer Close()

    // Create
    task := model.Task{
        Name:     "test-backup",
        CronExpr: "0 0 2 * * *",
        TaskType: "shell",
        Command:  "echo hello",
        Enabled:  true,
    }
    if err := DB.Create(&task).Error; err != nil {
        t.Fatalf("Create failed: %v", err)
    }
    if task.ID == 0 {
        t.Error("Task ID should be > 0 after create")
    }
    t.Logf("Created task ID=%d", task.ID)

    // Read
    var found model.Task
    if err := DB.First(&found, task.ID).Error; err != nil {
        t.Fatalf("Read failed: %v", err)
    }
    if found.Name != "test-backup" {
        t.Errorf("Name mismatch: want test-backup, got %s", found.Name)
    }
    t.Logf("Read ok: %s", found.Name)

    // Update
    newName := "updated-task"
    if err := DB.Model(&task).Update("name", newName).Error; err != nil {
        t.Fatalf("Update failed: %v", err)
    }
    DB.First(&found, task.ID)
    if found.Name != newName {
        t.Errorf("Update mismatch: want %s, got %s", newName, found.Name)
    }
    t.Logf("Update ok: %s", found.Name)

    // Delete
    if err := DB.Delete(&task).Error; err != nil {
        t.Fatalf("Delete failed: %v", err)
    }
    err := DB.First(&found, task.ID).Error
    if err == nil {
        t.Error("Record still exists after delete")
    }
    t.Log("Delete ok")
}
