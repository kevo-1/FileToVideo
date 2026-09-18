package infrastructure

import (
	"fmt"
	"sync"

	"github.com/kevo-1/FileToVideo/internal/repository"
	"github.com/kevo-1/FileToVideo/internal/services"
)

type Task struct {
	reqId string
	file  *repository.TempFile
}

type FileQueue struct {
	tasksQueue chan *Task
	wg         sync.WaitGroup
}

func NewFileQueue(workerCount, bufferSize int) *FileQueue {
	fq := &FileQueue{
		tasksQueue: make(chan *Task, bufferSize),
	}

	for i := 1; i <= workerCount; i++ {
		go fq.worker(i)
	}

	return fq
}

func (fq *FileQueue) worker(id int) {
	for task := range fq.tasksQueue {
		fmt.Printf("Worker %d processing task %s: %s\n", id, task.file.ReqId, task.file.FileName)
		//file with the processing
		services.ProcessFile(task.file)
		fq.wg.Done()
	}
}

func (fq *FileQueue) EnqueueTask(tempFile *repository.TempFile) {
	task := &Task{
		tempFile.ReqId,
		tempFile,
	}

	fq.wg.Add(1)
	fq.tasksQueue <- task
}

func (fq *FileQueue) Wait() {
	fq.wg.Wait()
}
