/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/test/e2e/framework"
	testutils "k8s.io/kubernetes/test/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// This test requires Rescheduler to be enabled.
var _ = framework.KubeDescribe("Rescheduler [Serial]", func() {
	f := framework.NewDefaultFramework("rescheduler")
	var ns string
	var totalMillicores int

	BeforeEach(func() {
		framework.SkipUnlessProviderIs("gce", "gke")
		ns = f.Namespace.Name
		nodes := framework.GetReadySchedulableNodesOrDie(f.Client)
		nodeCount := len(nodes.Items)
		Expect(nodeCount).NotTo(BeZero())

		cpu := nodes.Items[0].Status.Capacity[api.ResourceCPU]
		totalMillicores = int((&cpu).MilliValue()) * nodeCount
	})

	It("should ensure that critical pod is scheduled in case there is no resources available", func() {
		By("reserving all available cpu")
		err := reserveAllCpu(f, "reserve-all-cpu", totalMillicores)
		defer framework.DeleteRCAndPods(f.Client, f.ClientSet, ns, "reserve-all-cpu")
		framework.ExpectNoError(err)

		By("creating a new instance of DNS and waiting for DNS to be scheduled")
		label := labels.SelectorFromSet(labels.Set(map[string]string{"k8s-app": "kube-dns"}))
		listOpts := api.ListOptions{LabelSelector: label}
		rcs, err := f.Client.ReplicationControllers(api.NamespaceSystem).List(listOpts)
		framework.ExpectNoError(err)
		Expect(len(rcs.Items)).Should(Equal(1))

		rc := rcs.Items[0]
		replicas := uint(rc.Spec.Replicas)

		err = framework.ScaleRC(f.Client, f.ClientSet, api.NamespaceSystem, rc.Name, replicas+1, true)
		defer framework.ExpectNoError(framework.ScaleRC(f.Client, f.ClientSet, api.NamespaceSystem, rc.Name, replicas, true))
		framework.ExpectNoError(err)
	})
})

func reserveAllCpu(f *framework.Framework, id string, millicores int) error {
	timeout := 5 * time.Minute
	replicas := millicores / 100

	ReserveCpu(f, id, 1, 100)
	framework.ExpectNoError(framework.ScaleRC(f.Client, f.ClientSet, f.Namespace.Name, id, uint(replicas), false))

	for start := time.Now(); time.Since(start) < timeout; time.Sleep(10 * time.Second) {
		pods, err := framework.GetPodsInNamespace(f.Client, f.Namespace.Name, framework.ImagePullerLabels)
		if err != nil {
			return err
		}

		if len(pods) != replicas {
			continue
		}

		allRunningOrUnschedulable := true
		for _, pod := range pods {
			if !podRunningOrUnschedulable(pod) {
				allRunningOrUnschedulable = false
				break
			}
		}
		if allRunningOrUnschedulable {
			return nil
		}
	}
	return fmt.Errorf("Pod name %s: Gave up waiting %v for %d pods to come up", id, timeout, replicas)
}

func podRunningOrUnschedulable(pod *api.Pod) bool {
	_, cond := api.GetPodCondition(&pod.Status, api.PodScheduled)
	if cond != nil && cond.Status == api.ConditionFalse && cond.Reason == "Unschedulable" {
		return true
	}
	running, _ := testutils.PodRunningReady(pod)
	return running
}
