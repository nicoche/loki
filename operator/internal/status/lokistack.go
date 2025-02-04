package status

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"k8s.io/client-go/util/retry"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	messageReady   = "All components ready"
	messageFailed  = "Some LokiStack components failed"
	messagePending = "Some LokiStack components pending on dependencies"
)

// DegradedError contains information about why the managed LokiStack has an invalid configuration.
type DegradedError struct {
	Message string
	Reason  lokiv1.LokiStackConditionReason
	Requeue bool
}

func (e *DegradedError) Error() string {
	return fmt.Sprintf("cluster degraded: %s", e.Message)
}

// SetReadyCondition updates or appends the condition Ready to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetReadyCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	ready := metav1.Condition{
		Type:    string(lokiv1.ConditionReady),
		Message: messageReady,
		Reason:  string(lokiv1.ReasonReadyComponents),
	}

	return updateCondition(ctx, k, req, ready)
}

// SetFailedCondition updates or appends the condition Failed to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetFailedCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	failed := metav1.Condition{
		Type:    string(lokiv1.ConditionFailed),
		Message: messageFailed,
		Reason:  string(lokiv1.ReasonFailedComponents),
	}

	return updateCondition(ctx, k, req, failed)
}

// SetPendingCondition updates or appends the condition Pending to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetPendingCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	pending := metav1.Condition{
		Type:    string(lokiv1.ConditionPending),
		Message: messagePending,
		Reason:  string(lokiv1.ReasonPendingComponents),
	}

	return updateCondition(ctx, k, req, pending)
}

// SetDegradedCondition appends the condition Degraded to the lokistack status conditions.
func SetDegradedCondition(ctx context.Context, k k8s.Client, req ctrl.Request, msg string, reason lokiv1.LokiStackConditionReason) error {
	degraded := metav1.Condition{
		Type:    string(lokiv1.ConditionDegraded),
		Message: msg,
		Reason:  string(reason),
	}

	return updateCondition(ctx, k, req, degraded)
}

func updateCondition(ctx context.Context, k k8s.Client, req ctrl.Request, condition metav1.Condition) error {
	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup LokiStack", "name", req.NamespacedName)
	}

	for _, c := range stack.Status.Conditions {
		if c.Type == condition.Type &&
			c.Reason == condition.Reason &&
			c.Message == condition.Message &&
			c.Status == metav1.ConditionTrue {
			// resource already has desired condition
			return nil
		}
	}

	condition.Status = metav1.ConditionTrue

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
			return err
		}

		now := metav1.Now()
		condition.LastTransitionTime = now

		index := -1
		for i := range stack.Status.Conditions {
			// Reset all other conditions first
			stack.Status.Conditions[i].Status = metav1.ConditionFalse
			stack.Status.Conditions[i].LastTransitionTime = now

			// Locate existing pending condition if any
			if stack.Status.Conditions[i].Type == condition.Type {
				index = i
			}
		}

		if index == -1 {
			stack.Status.Conditions = append(stack.Status.Conditions, condition)
		} else {
			stack.Status.Conditions[index] = condition
		}

		return k.Status().Update(ctx, &stack)
	})
}
