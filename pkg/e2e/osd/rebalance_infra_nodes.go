package osd

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/osde2e/pkg/common/alert"
	"github.com/openshift/osde2e/pkg/common/helper"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"log"
	"os/exec"
	"time"
)

const (
	rebalanceInfraNodesTestName  = "[Suite: informing] [OSD] Rebalance Infra Nodes"
	rebalanceInfraNodesNamespace = "openshift-monitoring"
	splunkNamespace              = "openshift-security"
	rebalanceInfraNodesCronJob   = "osd-rebalance-infra-nodes"
	imbalanceScriptPath          = "scripts/imbalance-infra-nodes.sh"
	pollInterval                 = 10 * time.Second
	podSucceededTimeout          = 5 * time.Minute
)

func init() {
	alert.RegisterGinkgoAlert(rebalanceInfraNodesTestName, "SD-SREP", "Jing Zhang", "sd-cicd-alerts", "sd-cicd@redhat.com", 4)
}

var _ = ginkgo.Describe(rebalanceInfraNodesTestName, func() {
	h := helper.New()

	ginkgo.Context("re-balance the infra nodes with cronjob", func() {

		ginkgo.It("infra nodes should be rebalanced after executing the cronjob", func() {
			ginkgo.By("Putting the cluster into imbalanced state")
			output, err := exec.Command("/bin/sh", imbalanceScriptPath).Output()
			Expect(err).ToNot(HaveOccurred())
			log.Printf("Output for imbalancing infra nodes: \n%v\n", string(output))

			ginkgo.By("Creating job from CronJob to rebalance the infra workloads")
			cronjob, err := h.Kube().BatchV1beta1().CronJobs(rebalanceInfraNodesNamespace).Get(context.TODO(), rebalanceInfraNodesCronJob, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(cronjob).NotTo(BeNil())

			jobName := fmt.Sprintf("%s-manual-", rebalanceInfraNodesCronJob)
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: jobName,
					Namespace:    rebalanceInfraNodesNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "batch/v1beta1",
							Kind:       "CronJob",
							Name:       rebalanceInfraNodesCronJob,
							UID:        cronjob.GetUID(),
						},
					},
				},
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:    rebalanceInfraNodesCronJob,
									Image:   "image-registry.openshift-image-registry.svc:5000/openshift/cli:latest",
									Command: []string{"/bin/sh", "-c", "/etc/config/entrypoint"},
									VolumeMounts: []v1.VolumeMount{
										{
											Name:      rebalanceInfraNodesCronJob,
											MountPath: "/etc/config",
											ReadOnly:  true,
										},
									},
								},
							},
							RestartPolicy:      v1.RestartPolicyNever,
							ServiceAccountName: rebalanceInfraNodesCronJob,
							Volumes: []v1.Volume{
								{
									Name: rebalanceInfraNodesCronJob,
									VolumeSource: v1.VolumeSource{
										ConfigMap: &v1.ConfigMapVolumeSource{
											LocalObjectReference: v1.LocalObjectReference{
												Name: rebalanceInfraNodesCronJob,
											},
											DefaultMode: pointer.Int32Ptr(0755),
										},
									},
								},
							},
						},
					},
				},
			}
			job, err = h.Kube().BatchV1().Jobs(rebalanceInfraNodesNamespace).Create(context.TODO(), job, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(job).NotTo(BeNil())
			log.Printf("Created job %v from cronjob %v", job.Name, cronjob.Name)

			labelSelector := fmt.Sprintf("job-name=%s", job.GetName())
			listOptions := metav1.ListOptions{
				LabelSelector: labelSelector,
				Limit:         100,
			}
			pods, err := h.Kube().CoreV1().Pods(rebalanceInfraNodesNamespace).List(context.TODO(), listOptions)
			Expect(err).ToNot(HaveOccurred())

			var podName string
			for _, pod := range pods.Items {
				fmt.Printf("Pod %v status: %v\n", pod.Name, pod.Status.Phase)
				podName = pod.Name
			}
			Expect(podName).NotTo(BeNil())

			var pod *v1.Pod
			wait.PollImmediate(pollInterval, podSucceededTimeout, func() (bool, error) {
				pod, err = h.Kube().CoreV1().Pods(rebalanceInfraNodesNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if pod.Status.Phase == v1.PodSucceeded {
					return true, nil
				}
				return false, err
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(pod.Status.Phase).Should(Equal(v1.PodSucceeded))

			ginkgo.By("Verifying the infra nodes are rebalanced")
			listOptions = metav1.ListOptions{
				LabelSelector: "node-role.kubernetes.io=infra",
				Limit:         100,
			}
			infraNodeList, err := h.Kube().CoreV1().Nodes().List(context.TODO(), listOptions)
			Expect(err).ToNot(HaveOccurred())
			Expect(infraNodeList).NotTo(BeNil())

			for _, node := range infraNodeList.Items {
				log.Printf("Verifying infra node: %v\n", node.Name)

				podsNumber := checkPodsBalance(h, rebalanceInfraNodesNamespace, "app", "alertmanager", node.Name)
				max := 1
				if len(infraNodeList.Items) < 3 {
					max = 2
				}
				Expect(podsNumber).To(BeNumerically("<=", max))

				podsNumber = checkPodsBalance(h, rebalanceInfraNodesNamespace, "app", "prometheus", node.Name)
				Expect(podsNumber).To(BeNumerically("<=", 1))

				podsNumber = checkPodsBalance(h, splunkNamespace, "name", "splunk-heavy-forwarder", node.Name)
				Expect(podsNumber).To(BeNumerically("<=", 1))
			}
		})
	})
})

func checkPodsBalance(h *helper.H, namespace, labelName, workloadName, nodeName string) int {
	labelSelector := fmt.Sprintf("%s=%s", labelName, workloadName)
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", nodeName)

	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
		Limit:         100,
	}

	pods, _ := h.Kube().CoreV1().Pods(namespace).List(context.TODO(), listOptions)
	fmt.Printf("%v pods on node: %v\n", len(pods.Items), nodeName)
	for _, pod := range pods.Items {
		fmt.Printf("pod: %v on node: %v\n", pod.Name, nodeName)
	}

	return len(pods.Items)
}
