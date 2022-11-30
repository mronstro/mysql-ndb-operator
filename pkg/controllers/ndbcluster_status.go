// Copyright (c) 2022, Oracle and/or its affiliates.
//
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/

package controllers

import (
	"fmt"
	"strings"

	v1 "github.com/mysql/ndb-operator/pkg/apis/ndbcontroller/v1"
	"github.com/mysql/ndb-operator/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// statusEqual checks if the given two NdbClusterStatuses are equal.
// This function does not compare all the fields of the conditions
// as they are already dependent on the Status field.
func statusEqual(oldStatus *v1.NdbClusterStatus, newStatus *v1.NdbClusterStatus) bool {
	return oldStatus.ProcessedGeneration == newStatus.ProcessedGeneration &&
		oldStatus.ReadyManagementNodes == newStatus.ReadyManagementNodes &&
		oldStatus.ReadyDataNodes == newStatus.ReadyDataNodes &&
		oldStatus.ReadyMySQLServers == newStatus.ReadyMySQLServers &&
		oldStatus.GeneratedRootPasswordSecretName == newStatus.GeneratedRootPasswordSecretName &&
		// TODO: Improve this comparison when more conditions are added
		oldStatus.Conditions[0].Status == newStatus.Conditions[0].Status &&
		oldStatus.Conditions[0].Reason == newStatus.Conditions[0].Reason &&
		oldStatus.Conditions[0].Message == newStatus.Conditions[0].Message
}

// calculateNdbClusterStatus generates the current status for the NdbCluster in SyncContext
func (sc *SyncContext) calculateNdbClusterStatus() *v1.NdbClusterStatus {

	// Generate status for the NdbCluster resource
	nc := sc.ndb
	status := &v1.NdbClusterStatus{}

	// Generate Management, Data Nodes and MySQL Servers status fields
	// Node ready status for Management Nodes
	numOfReadyMgmdNodes := int32(0)
	if sc.mgmdNodeSfset != nil {
		numOfReadyMgmdNodes = sc.mgmdNodeSfset.Status.ReadyReplicas
	}
	status.ReadyManagementNodes = fmt.Sprintf(
		"Ready:%d/%d", numOfReadyMgmdNodes, nc.GetManagementNodeCount())

	// Node ready status for Data Nodes
	numOfReadyDataNodes := int32(0)
	if sc.dataNodeSfSet != nil {
		numOfReadyDataNodes = sc.dataNodeSfSet.Status.ReadyReplicas
	}
	status.ReadyDataNodes = fmt.Sprintf(
		"Ready:%d/%d", numOfReadyDataNodes, nc.Spec.DataNode.NodeCount)

	// Node ready status and generatedRootPasswordSecretName for MySQL Servers
	numOfReadyMySQLNodes := int32(0)
	numOfMySQLServersRequired := nc.GetMySQLServerNodeCount()
	if sc.mysqldSfset != nil {
		numOfReadyMySQLNodes = sc.mysqldSfset.Status.ReadyReplicas
		// Update generatedRootPasswordSecretName if one exists
		if numOfMySQLServersRequired > 0 {
			if secretName, customSecret := resources.GetMySQLRootPasswordSecretName(nc); !customSecret {
				// The secret has been generated by the controller
				status.GeneratedRootPasswordSecretName = secretName
			}
		}
	}
	status.ReadyMySQLServers = fmt.Sprintf(
		"Ready:%d/%d", numOfReadyMySQLNodes, numOfMySQLServersRequired)

	// Set processedGeneration and upToDate condition
	upToDateCondition := v1.NdbClusterCondition{
		Type:               v1.NdbClusterUpToDate,
		LastTransitionTime: metav1.Now(),
	}
	if sc.syncSuccess {
		status.ProcessedGeneration = nc.Generation
		// Set the NdbClusterUpToDate condition
		upToDateCondition.Status = corev1.ConditionTrue
		upToDateCondition.Reason = v1.NdbClusterUptoDateReasonSyncSuccess
		upToDateCondition.Message = fmt.Sprintf(
			"NdbCluster Spec generation %d was successfully applied to the MySQL Cluster",
			status.ProcessedGeneration)
	} else {
		// The sync is ongoing
		status.ProcessedGeneration = nc.Status.ProcessedGeneration

		upToDateCondition.Status = corev1.ConditionFalse
		if errMsgs := sc.retrievePodErrors(); errMsgs != nil {
			// One or more pods owned by the NdbCluster resource is failing
			klog.Errorf("One or more pods owned by the ndbcluster resource %q are failing : \n%s", getNamespacedName(nc), errMsgs)
			upToDateCondition.Reason = v1.NdbClusterUptoDateReasonError
			upToDateCondition.Message = strings.Join(errMsgs, "\n")
		} else if nc.Generation == 1 {
			// The MySQL Cluster nodes are being started for the first time
			upToDateCondition.Reason = v1.NdbClusterUptoDateReasonISR
			upToDateCondition.Message = "MySQL Cluster is starting up"
		} else {
			// Config change is being applied to the nodes
			upToDateCondition.Reason = v1.NdbClusterUptoDateReasonSpecUpdateInProgress
			upToDateCondition.Message = fmt.Sprintf(
				"NdbCluster spec generation %d is being applied to the MySQL Cluster", nc.Generation)
		}
	}
	status.Conditions = append(status.Conditions, upToDateCondition)

	return status
}
