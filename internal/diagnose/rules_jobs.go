package diagnose

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

// jobFailed reports a terminally failed job: failures recorded, nothing
// still running, nothing ever succeeded. Active or partially succeeding jobs
// are somebody else's (usually the pod rules') problem.
func jobFailed(j *batchv1.Job) bool {
	return j.Status.Failed > 0 && j.Status.Active == 0 && j.Status.Succeeded == 0
}

type JobFailedRule struct{}

func (JobFailedRule) Name() string { return "job_failed" }

func (JobFailedRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, j := range s.Jobs {
		if !jobFailed(&j) {
			continue
		}
		limit := ""
		if j.Spec.BackoffLimit != nil {
			limit = fmt.Sprintf("%d", *j.Spec.BackoffLimit)
		}
		ev := map[string]any{
			"failed":    j.Status.Failed,
			"succeeded": j.Status.Succeeded,
		}
		if limit != "" {
			ev["backoff_limit"] = limit
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("job/%s", j.Name),
			Namespace: j.Namespace,
			Reason:    "job_failed",
			Message:   fmt.Sprintf("Job failed %d time(s) with no successes", j.Status.Failed),
			Evidence:  ev,
			Recommendation: "Check the failed pods' logs — jobs don't retry forever, " +
				"and the backoff limit is the budget. Fix the cause or raise the limit.",
		})
	}
	return out
}

type CronJobFailingRule struct{}

func (CronJobFailingRule) Name() string { return "cronjob_failing" }

func (CronJobFailingRule) Run(s *k8s.Snapshot) []model.Finding {
	// Latest terminally-failed job per owning CronJob.
	failedByCron := map[string]*batchv1.Job{}
	for _, j := range s.Jobs {
		if !jobFailed(&j) {
			continue
		}
		for _, o := range j.OwnerReferences {
			if o.Kind != "CronJob" {
				continue
			}
			key := j.Namespace + "/" + o.Name
			if cur, ok := failedByCron[key]; !ok || j.CreationTimestamp.After(cur.CreationTimestamp.Time) {
				j := j
				failedByCron[key] = &j
			}
		}
	}
	var out []model.Finding
	for _, cj := range s.CronJobs {
		failed := failedByCron[cj.Namespace+"/"+cj.Name]
		if failed == nil {
			continue
		}
		// A success after the failure means the schedule recovered.
		if cj.Status.LastSuccessfulTime != nil &&
			!cj.Status.LastSuccessfulTime.Before(&failed.CreationTimestamp) {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("cronjob/%s", cj.Name),
			Namespace: cj.Namespace,
			Reason:    "cronjob_failing",
			Message:   fmt.Sprintf("Latest run (%s) failed with no success since", failed.Name),
			Evidence: map[string]any{
				"failed_job": failed.Name,
				"schedule":   cj.Spec.Schedule,
			},
			Recommendation: "Check the failed job's pod logs; if the schedule itself is wrong, fix the cron expression.",
		})
	}
	return out
}

// Flags paused schedules. Missed backups hide here.
type CronJobSuspendedRule struct{}

func (CronJobSuspendedRule) Name() string { return "cronjob_suspended" }

func (CronJobSuspendedRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, cj := range s.CronJobs {
		if cj.Spec.Suspend == nil || !*cj.Spec.Suspend {
			continue
		}
		ev := map[string]any{"schedule": cj.Spec.Schedule}
		if cj.Status.LastScheduleTime != nil {
			ev["last_schedule"] = cj.Status.LastScheduleTime.Time.UTC().Format("2006-01-02 15:04")
		} else {
			ev["last_schedule"] = "never"
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("cronjob/%s", cj.Name),
			Namespace: cj.Namespace,
			Reason:    "cronjob_suspended",
			Message:   "CronJob is suspended — the schedule runs nothing",
			Evidence:  ev,
			Recommendation: "Resume it (`suspend: false`) if the pause was temporary, or delete it if the " +
				"job is retired — a paused schedule that nobody remembers is how backups silently stop.",
		})
	}
	return out
}
