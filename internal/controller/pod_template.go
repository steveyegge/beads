package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// DefaultImage is the default container image for agent pods.
	DefaultImage = "909418727440.dkr.ecr.us-east-1.amazonaws.com/gastown-agent:latest"

	// DefaultScreenPort is the screen terminal server port.
	DefaultScreenPort = 9400

	// LabelApp is the standard app label for agent pods.
	LabelApp = "app"
	// LabelAppValue is the value for the app label.
	LabelAppValue = "gastown-agent"
	// LabelRole is the Gas Town role label.
	LabelRole = "gastown.io/role"
	// LabelRig is the Gas Town rig label.
	LabelRig = "gastown.io/rig"
	// LabelAgent is the Gas Town agent ID label.
	LabelAgent = "gastown.io/agent"
	// LabelManagedBy identifies the controller.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// LabelManagedByValue is the value for managed-by.
	LabelManagedByValue = "agent-controller"
)

// PodTemplateConfig holds configurable values for building pod specs.
type PodTemplateConfig struct {
	Image         string
	Namespace     string
	BDDaemonHost  string
	BDDaemonPort  string
	APIKeySecret  string // Name of the K8s secret containing ANTHROPIC_API_KEY
}

// BuildPodSpec creates a K8s Pod spec matching the agent-pod Helm chart template.
func BuildPodSpec(agentID, role, rig string, cfg PodTemplateConfig) *corev1.Pod {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	labels := map[string]string{
		LabelApp:       LabelAppValue,
		LabelRole:      role,
		LabelRig:       rig,
		LabelAgent:     agentID,
		LabelManagedBy: LabelManagedByValue,
	}

	// Pod name is derived from agent ID (sanitized for K8s naming)
	podName := "agent-" + agentID

	var agentUID int64 = 1000
	var agentGID int64 = 1000
	nonRoot := true
	noPrivEsc := false

	env := []corev1.EnvVar{
		{Name: "GT_ROLE", Value: role},
		{Name: "GT_RIG", Value: rig},
		{Name: "HOME", Value: "/home/agent"},
		{Name: "SCREENRC_SCROLLBACK", Value: "10000"},
	}

	if cfg.BDDaemonHost != "" {
		env = append(env, corev1.EnvVar{Name: "BD_DAEMON_HOST", Value: cfg.BDDaemonHost})
	}
	if cfg.BDDaemonPort != "" {
		env = append(env, corev1.EnvVar{Name: "BD_DAEMON_PORT", Value: cfg.BDDaemonPort})
	}

	// Mount ANTHROPIC_API_KEY from secret if configured
	if cfg.APIKeySecret != "" {
		env = append(env, corev1.EnvVar{
			Name: "ANTHROPIC_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cfg.APIKeySecret,
					},
					Key: "ANTHROPIC_API_KEY",
				},
			},
		})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: cfg.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup:    &agentGID,
				RunAsNonRoot: &nonRoot,
			},
			TerminationGracePeriodSeconds: int64Ptr(30),
			Containers: []corev1.Container{
				{
					Name:            "agent",
					Image:           image,
					ImagePullPolicy: corev1.PullAlways,
					Env:             env,
					Ports: []corev1.ContainerPort{
						{
							Name:          "screen",
							ContainerPort: DefaultScreenPort,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:                &agentUID,
						RunAsGroup:               &agentGID,
						AllowPrivilegeEscalation: &noPrivEsc,
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt32(DefaultScreenPort),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       5,
						TimeoutSeconds:      5,
						FailureThreshold:    30,
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt32(DefaultScreenPort),
							},
						},
						InitialDelaySeconds: 10,
						PeriodSeconds:       30,
						TimeoutSeconds:      5,
						FailureThreshold:    3,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt32(DefaultScreenPort),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       10,
						TimeoutSeconds:      5,
						FailureThreshold:    3,
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/home/agent/gt",
						},
						{
							Name:      "tmp",
							MountPath: "/tmp",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}
