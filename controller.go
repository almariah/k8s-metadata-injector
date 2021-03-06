package main

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/fields"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	ebsTagsAnnotationKey = "ebs-tagger.kubernetes.io/ebs-additional-resource-tags"
)

const (
	resyncPeriod = 30 * time.Minute
	maxRetries   = 5
)

type Controller struct {
	clientset   kubernetes.Interface
	pvcInformer cache.SharedIndexInformer
	pvInformer  cache.SharedIndexInformer
	queue       workqueue.RateLimitingInterface
}

type Task struct {
	Key    string
	Action string
}

func NewController(kubeclientset kubernetes.Interface) *Controller {

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	listwatchPVC := cache.NewListWatchFromClient(kubeclientset.CoreV1().RESTClient(), "persistentvolumeclaims", metav1.NamespaceAll, fields.Everything())
	listwatchPV := cache.NewListWatchFromClient(kubeclientset.CoreV1().RESTClient(), "persistentvolumes", metav1.NamespaceAll, fields.Everything())

	pvci := cache.NewSharedIndexInformer(
		listwatchPVC,
		&corev1.PersistentVolumeClaim{},
		resyncPeriod,
		cache.Indexers{},
	)

	pvi := cache.NewSharedIndexInformer(
		listwatchPV,
		&corev1.PersistentVolume{},
		resyncPeriod,
		cache.Indexers{},
	)

	pvi.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				pv := obj.(*corev1.PersistentVolume)
				if pv.Spec.PersistentVolumeSource.AWSElasticBlockStore == nil {
					return
				}
				queue.AddRateLimited(Task{
					Key:    key,
					Action: "CREATE",
				})
			} else {
				runtime.HandleError(err)
				return
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				pvNew := new.(*corev1.PersistentVolume)
				pvOld := old.(*corev1.PersistentVolume)
				if pvNew.ResourceVersion == pvOld.ResourceVersion {
					return
				}
				if pvNew.Spec.PersistentVolumeSource.AWSElasticBlockStore == nil {
					return
				}
				queue.AddRateLimited(Task{
					Key:    key,
					Action: "UPDATE",
				})
			} else {
				runtime.HandleError(err)
				return
			}
		},
	})

	return &Controller{
		clientset:   kubeclientset,
		pvcInformer: pvci,
		pvInformer:  pvi,
		queue:       queue,
	}
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Info("Starting ebs-tagger controller")
	defer klog.Infof("Shutting down ebs-tagger controller")

	go c.pvcInformer.Run(stopCh)
	go c.pvInformer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")

	if !cache.WaitForCacheSync(stopCh, c.pvcInformer.HasSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for PVC caches to sync"))
		return
	}

	if !cache.WaitForCacheSync(stopCh, c.pvInformer.HasSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for PV caches to sync"))
		return
    }

	klog.Info("Starting workers")
	// Launch two workers to process Foo resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	// wait until we're told to stop
	<-stopCh
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNext() {
	}
}

// processNext will read a single work item off the workqueue and
// attempt to process it, by calling the process.
func (c *Controller) processNext() bool {
	key, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.process(key.(Task))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		klog.Infof("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		// err != nil and too many retries
		klog.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		runtime.HandleError(err)
	}

	return true
}

func (c *Controller) process(task Task) error {

	obj, exists, err := c.pvInformer.GetIndexer().GetByKey(task.Key)
	if err != nil {
		return fmt.Errorf("failed to retrieve pv by key %q: %v", task.Key, err)
	}

	if exists {

		pv := obj.(*corev1.PersistentVolume)

		volume := pv.Spec.PersistentVolumeSource.AWSElasticBlockStore.VolumeID
		if volume == "" {
			return nil
		}

		var volumeID string
		volumeIDList := strings.Split(volume, "/")

		if len(volumeIDList) == 4 {
			volumeID = volumeIDList[3]
		} else if len(volumeIDList) == 1 {
			volumeID = volumeIDList[0]
		} else {
			return fmt.Errorf("failed to parse EBS %q", volume)
		}

		if pv.Spec.ClaimRef == nil {
			return nil
		}

		if pv.Spec.ClaimRef.Kind != "PersistentVolumeClaim" {
			return nil
		}

		pvcName := pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name

		objPVC, existsPVC, err := c.pvcInformer.GetIndexer().GetByKey(pvcName)
		if err != nil {
			return fmt.Errorf("failed to retrieve pvc by key %q: %v", task.Key, err)
		}

		if existsPVC {
			pvc := objPVC.(*corev1.PersistentVolumeClaim)

			if annotation, ok := pvc.Annotations[ebsTagsAnnotationKey]; ok {
				tags := getEBSTags(annotation)
				if tags != nil {
					err = createTags(&volumeID, tags)
					if err != nil {
						return err
					}
					klog.Infof("Tags created for EBS %q (%q)!", volumeID, pvcName)
				}
			}
		}
	}

	return nil

}
