package deployment

import (
	"context"
	"fmt"

	"github.com/nut-api/publish-routing-controller.git/pkg/webrenderer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ webrenderer.Webrenderer = (*WebrendererDeployment)(nil)

type WebrendererDeployment struct {
	Client             client.Client
	DeploymentName     string
	ServiceName        string
	WebrendererVersion string
}

func (d *WebrendererDeployment) GetAndCreateIfNotExists(ctx context.Context) error {
	l := logf.FromContext(ctx)
	deployment := &appsv1.Deployment{}
	err := d.Client.Get(ctx, client.ObjectKey{
		Name:      d.DeploymentName,
		Namespace: "default",
	}, deployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		// Update deployment with the desired spec
		newDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      d.DeploymentName,
				Namespace: "default",
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"webrenderer-version": d.WebrendererVersion},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"webrenderer-version": d.WebrendererVersion},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "webrenderer",
								Image: "argoproj/rollouts-demo:" + d.WebrendererVersion,
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										Protocol:      corev1.ProtocolTCP,
										ContainerPort: 8080,
									},
								},
							},
						},
					},
				},
			},
		}
		err = d.Client.Create(ctx, newDeployment)
		if err != nil {
			return err
		}
		l.Info("Created Deployment", "name", d.DeploymentName)
	} else {
		// Deployment exists
		l.Info("Deployment already exists", "name", d.DeploymentName)
	}
	service := &corev1.Service{}
	err = d.Client.Get(ctx, client.ObjectKey{
		Name:      d.ServiceName,
		Namespace: "default",
	}, service)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      d.ServiceName,
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"webrenderer-version": d.WebrendererVersion},
				Ports: []corev1.ServicePort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8080,
					},
				},
				Type: corev1.ServiceTypeClusterIP,
			},
		}
		err = d.Client.Create(ctx, service)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *WebrendererDeployment) DeleteWebrenderer(ctx context.Context) error {
	fmt.Println("Deleting webrenderer deployment and service:", d.DeploymentName, d.ServiceName)

	deployment := &appsv1.Deployment{}
	err := d.Client.Get(ctx, client.ObjectKey{
		Name:      d.DeploymentName,
		Namespace: "default",
	}, deployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, nothing to delete
	} else {
		// Found, delete it
		err = d.Client.Delete(ctx, deployment)
		if err != nil {
			return err
		}
	}

	service := &corev1.Service{}
	err = d.Client.Get(ctx, client.ObjectKey{
		Name:      d.ServiceName,
		Namespace: "default",
	}, service)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, nothing to delete
	} else {
		// Found, delete it
		err = d.Client.Delete(ctx, service)
		if err != nil {
			return err
		}
	}
	return nil
}

func int32Ptr(i int32) *int32 { return &i }
