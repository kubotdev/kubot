package diagnose

func DefaultRules() []Rule {
	return []Rule{
		PodCrashLoopRule{},
		PodOOMKilledRule{},
		PodImagePullRule{},
		PodPendingRule{},
		PodProbeFailingRule{},
		DeploymentUnavailableRule{},
		StatefulSetUnavailableRule{},
		DaemonSetUnavailableRule{},
		ServiceNoEndpointsRule{},
		PodMissingResourcesRule{},
		PodLowLimitRule{},
		PodNearLimitRule{},
		DeploymentResourceRiskRule{},
		DeploymentStalledRule{},
		DeploymentRolloutRecentRule{},
		ReplicaSetLeftoverRule{},
		PVCPendingRule{},
		PodMountFailureRule{},
		NodePressureRule{},
		IngressNoBackendsRule{},
		IngressClassRule{},
		IngressTLSSecretRule{},
		JobFailedRule{},
		CronJobFailingRule{},
		CronJobSuspendedRule{},
		PodHostAccessRule{},
		WorkloadRootRule{},
		HPAAtMaxRule{},
		HPANoopRule{},
		PDBBlockingRule{},
		PDBNoMatchRule{},
	}
}

func CheckedSubsystems() []string {
	return []string{"pods", "deployments", "statefulsets", "daemonsets", "services", "ingresses", "jobs", "nodes", "events"}
}
