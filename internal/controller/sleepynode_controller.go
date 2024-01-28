/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SleepyNodeReconciler reconciles a SleepyNode object
type SleepyNodeReconciler struct {
	ctx    context.Context
	cancel context.CancelFunc

	client client.Client
	scheme *runtime.Scheme
	timer  *time.Timer
	ticker *time.Ticker
	now    time.Time
}

const powerOffTimeout = 30 * time.Second

func New(ctx context.Context, c client.Client, s *runtime.Scheme) *SleepyNodeReconciler {
	var (
		timer  = time.NewTimer(powerOffTimeout)
		ticker = time.NewTicker(1 * time.Second)
	)

	ctx2, cancel := context.WithCancel(ctx)

	var r = &SleepyNodeReconciler{
		ctx:    ctx2,
		cancel: cancel,

		client: c,
		scheme: s,
		timer:  timer,
		ticker: ticker,
		now:    time.Now(),
	}

	go r.startTimer()

	return r
}

func (r *SleepyNodeReconciler) Stop() error {
	r.cancel()

	return nil
}

func (r *SleepyNodeReconciler) startTimer() {
	defer r.timer.Stop()
	defer r.ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			break

		case <-r.ticker.C:
			fmt.Println("tick:", int64(powerOffTimeout.Seconds()-time.Since(r.now).Seconds()))

		case <-r.timer.C:
			r.ticker.Stop()
			fmt.Println("turning off node")

			// turn off GPU node
			var res, err = http.Get("http://10.32.0.66:5000/power-off")
			defer res.Body.Close()

			if err != nil {
				log.Log.Error(err, "Unable to power off the GPU node")
			}
		}
	}
}

//+kubebuilder:rbac:groups=batch.6740.io,resources=sleepynodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.6740.io,resources=sleepynodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.6740.io,resources=sleepynodes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SleepyNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *SleepyNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var (
		pod    corev1.Pod
		filter = client.ObjectKey{
			Name:      req.Name,
			Namespace: req.Namespace,
		}

		err = r.client.Get(ctx, filter, &pod)
	)

	if err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		log.Log.Error(err, "unable to list pods")
		return ctrl.Result{}, err
	}

	if pod.Status.Phase != corev1.PodPending && pod.Status.Phase != corev1.PodRunning {
		return ctrl.Result{}, nil
	}

	for _, cont := range pod.Spec.Containers {
		var _, ok = cont.Resources.Limits["nvidia.com/gpu"]
		if ok {
			fmt.Println("resetting timer")
			r.timer.Reset(powerOffTimeout)
			r.ticker.Reset(1 * time.Second)
			r.now = time.Now()

			log.Log.Info("Powering on the GPU node for pod", "name", req.Name)

			// TODO: use an env variable for the IP
			var res, err = http.Get("http://10.32.0.66:5000/power-on")
			defer res.Body.Close()

			if err != nil {
				log.Log.Error(err, "Unable to power on the GPU node")
				// TODO: add stuff just in case later
				return ctrl.Result{}, err
			}

			break
		}
	}

	return ctrl.Result{
		RequeueAfter: 5 * time.Second,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SleepyNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
