// Package worker — фоновые воркеры синхронизации с Odds-API. Воркеры работают
// как goroutines внутри основного процесса, расписание — через robfig/cron/v3.
//
// Слой воркеров использует те же репозитории и клиент oddsapi, что и остальное
// приложение; бизнес-логику HTTP-слоя он не трогает. Каждая итерация воркера
// получает собственный context с таймаутом и уважает его отмену.
package worker

import (
	"context"
	"log"
	"time"

	"github.com/robfig/cron/v3"
)

// Job — одна периодическая задача воркера: имя (для логов), cron-спека
// расписания, таймаут одной итерации и сама функция итерации.
type Job struct {
	Name     string
	Schedule string                          // cron-спека: "@every 1h", "0 * * * *" и т.п.
	Timeout  time.Duration                   // ограничение длительности одной итерации
	Run      func(ctx context.Context) error // одна итерация воркера
}

// Runner запускает зарегистрированные задачи по расписанию. Потокобезопасен в
// рамках жизненного цикла Start → Stop (повторный Start не предусмотрен).
type Runner struct {
	cron   *cron.Cron
	logger *log.Logger

	// baseCtx отменяется при Stop, чтобы прервать текущие итерации воркеров.
	baseCtx context.Context
	cancel  context.CancelFunc
}

// NewRunner создаёт раннер. SkipIfStillRunning гарантирует, что новая итерация
// не стартует, пока не завершилась предыдущая (защита от наложения при долгих
// запросах к API или лагах БД).
func NewRunner(logger *log.Logger) *Runner {
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.PrintfLogger(logger)),
	))
	return &Runner{
		cron:    c,
		logger:  logger,
		baseCtx: ctx,
		cancel:  cancel,
	}
}

// Register добавляет задачу в расписание. Вызывать до Start. Возвращает ошибку
// при некорректной cron-спеке.
func (r *Runner) Register(j Job) error {
	_, err := r.cron.AddFunc(j.Schedule, func() { r.runOnce(j) })
	if err != nil {
		return err
	}
	r.logger.Printf("worker: registered %q on schedule %q", j.Name, j.Schedule)
	return nil
}

// runOnce выполняет одну итерацию задачи с собственным context-таймаутом и
// логирует результат. Паники внутри воркеров cron перехватывает на уровне
// обёртки Recover при необходимости; здесь мы фиксируем длительность и ошибку.
func (r *Runner) runOnce(j Job) {
	ctx, cancel := context.WithTimeout(r.baseCtx, j.Timeout)
	defer cancel()

	start := time.Now()
	r.logger.Printf("worker %s: iteration started", j.Name)
	if err := j.Run(ctx); err != nil {
		r.logger.Printf("worker %s: iteration failed after %s: %v", j.Name, time.Since(start).Round(time.Millisecond), err)
		return
	}
	r.logger.Printf("worker %s: iteration done in %s", j.Name, time.Since(start).Round(time.Millisecond))
}

// Start запускает планировщик. Итерации начнут выполняться по расписанию.
func (r *Runner) Start() {
	r.cron.Start()
	r.logger.Printf("worker: runner started")
}

// Stop останавливает планировщик и дожидается завершения текущих итераций
// (с учётом отмены baseCtx) либо истечения timeout. Безопасно вызывать один раз
// при graceful shutdown.
func (r *Runner) Stop(timeout time.Duration) {
	// Прекращаем планирование новых запусков; cron.Stop() возвращает context,
	// который закрывается, когда уже запущенные задачи завершатся.
	stopped := r.cron.Stop()

	// Отменяем baseCtx, чтобы текущие итерации прервали сетевые/БД-ожидания.
	r.cancel()

	select {
	case <-stopped.Done():
	case <-time.After(timeout):
		r.logger.Printf("worker: stop timeout %s exceeded, abandoning in-flight iterations", timeout)
	}
	r.logger.Printf("worker: runner stopped")
}
