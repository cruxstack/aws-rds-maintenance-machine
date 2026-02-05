package mock

import (
	"strings"
	"time"
)

// runStateTransitions runs in the background and transitions resources through their states.
func (s *State) runStateTransitions() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.processTransitions()
		}
	}
}

// processTransitions checks all resources and transitions their states if ready.
func (s *State) processTransitions() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	waitDuration := s.getWaitDurationLocked()

	// Process instances
	for id, inst := range s.instances {
		// Check if fault injection is blocking this transition
		if s.faults.CheckStateTransition(id) {
			continue
		}

		// Handle pending status change (simulates AWS async status propagation)
		if inst.PendingStatusChange != "" && now.After(inst.PendingStatusChangeAt) {
			inst.Status = inst.PendingStatusChange
			inst.StatusChangedAt = now
			inst.PendingStatusChange = ""
			inst.PendingStatusChangeAt = time.Time{}
		}

		elapsed := now.Sub(inst.StatusChangedAt)

		// Handle deletion separately as it removes the instance
		if inst.Status == "deleting" || inst.Status == "delete-precheck" {
			if elapsed >= waitDuration {
				// Remove from cluster members
				if cluster, ok := s.clusters[inst.ClusterID]; ok {
					newMembers := make([]string, 0, len(cluster.Members)-1)
					for _, m := range cluster.Members {
						if m != id {
							newMembers = append(newMembers, m)
						}
					}
					cluster.Members = newMembers
				}
				// Remove instance
				delete(s.instances, id)
			}
			continue
		}

		// Handle stopped/stopping - these don't transition to available
		if inst.Status == "stopped" || inst.Status == "stopping" {
			if inst.Status == "stopping" && elapsed >= waitDuration {
				inst.Status = "stopped"
				inst.StatusChangedAt = now
			}
			continue
		}

		// Handle all transitional statuses that eventually become available
		if IsTransitionalStatus(inst.Status) {
			if elapsed >= waitDuration {
				// Check if there's a pending transitional status to go through first
				if inst.TransitionalStatus != "" {
					inst.Status = inst.TransitionalStatus
					inst.TransitionalStatus = ""
					inst.StatusChangedAt = now
				} else {
					// Apply pending modifications if this was a modifying status
					if inst.Status == "modifying" {
						if inst.PendingInstanceType != "" {
							inst.InstanceType = inst.PendingInstanceType
							inst.PendingInstanceType = ""
						}
						if inst.PendingStorageType != "" {
							inst.StorageType = inst.PendingStorageType
							inst.PendingStorageType = ""
						}
						if inst.PendingIOPS != nil {
							inst.IOPS = inst.PendingIOPS
							inst.PendingIOPS = nil
						}
					}
					// Enable Performance Insights after configuring-performance-insights completes
					if inst.Status == "configuring-performance-insights" {
						inst.PerformanceInsightsEnabled = true
					}
					inst.Status = "available"
					inst.StatusChangedAt = now
				}
			}
		}
	}

	// Process clusters
	for id, cluster := range s.clusters {
		// Check if fault injection is blocking this transition
		if s.faults.CheckStateTransition(id) {
			continue
		}

		elapsed := now.Sub(cluster.StatusChangedAt)

		switch cluster.Status {
		case "deleting":
			if elapsed >= waitDuration {
				// Check if all instances are also deleted (or don't exist)
				allDeleted := true
				for _, memberID := range cluster.Members {
					if _, ok := s.instances[memberID]; ok {
						allDeleted = false
						break
					}
				}
				if allDeleted {
					delete(s.clusters, id)
				}
			}
		case "modifying", "upgrading":
			if elapsed >= waitDuration {
				// Check if all instances are also available
				allAvailable := true
				for _, memberID := range cluster.Members {
					if inst, ok := s.instances[memberID]; ok {
						if inst.Status != "available" {
							allAvailable = false
							break
						}
					}
				}
				if allAvailable {
					cluster.Status = "available"
					cluster.StatusChangedAt = now
				}
			}
		}
	}

	// Process snapshots
	for id, snap := range s.snapshots {
		// Check if fault injection is blocking this transition
		if s.faults.CheckStateTransition(id) {
			continue
		}

		elapsed := now.Sub(snap.StatusChangedAt)

		switch snap.Status {
		case "creating":
			if elapsed >= waitDuration {
				snap.Status = "available"
				snap.StatusChangedAt = now
			}
		}
	}

	// Process Blue-Green deployments
	for id, bg := range s.blueGreenDeployments {
		// Check if fault injection is blocking this transition
		if s.faults.CheckStateTransition(id) {
			continue
		}

		elapsed := now.Sub(bg.StatusChangedAt)

		switch bg.Status {
		case "PROVISIONING":
			// Progress through tasks
			allTasksComplete := true
			for i := range bg.Tasks {
				if bg.Tasks[i].Status == "PENDING" {
					bg.Tasks[i].Status = "IN_PROGRESS"
					allTasksComplete = false
					break
				} else if bg.Tasks[i].Status == "IN_PROGRESS" {
					if elapsed >= waitDuration {
						bg.Tasks[i].Status = "COMPLETED"
						bg.StatusChangedAt = now
					}
					allTasksComplete = false
					break
				}
			}
			if allTasksComplete {
				// Extract source cluster info and create green cluster/instances
				sourceClusterID := extractClusterIDFromARN(bg.SourceClusterARN)
				if _, ok := s.clusters[sourceClusterID]; ok {
					// Create green cluster (simulated - in real AWS this is automatic)
					greenClusterID := sourceClusterID + "-green-mock"
					bg.TargetClusterARN = "arn:aws:rds:us-east-1:123456789012:cluster:" + greenClusterID

					// Update switchover details with target members
					for i := range bg.SwitchoverDetails {
						if bg.SwitchoverDetails[i].SourceMember == bg.SourceClusterARN {
							bg.SwitchoverDetails[i].TargetMember = bg.TargetClusterARN
						} else {
							// Instance - add -green-mock suffix
							sourceMember := bg.SwitchoverDetails[i].SourceMember
							parts := strings.Split(sourceMember, ":db:")
							if len(parts) == 2 {
								bg.SwitchoverDetails[i].TargetMember = parts[0] + ":db:" + parts[1] + "-green-mock"
							}
						}
						bg.SwitchoverDetails[i].Status = "AVAILABLE"
					}
				}

				bg.Status = "AVAILABLE"
				bg.StatusDetails = "Green environment ready for switchover"
				bg.StatusChangedAt = now
			}

		case "SWITCHOVER_IN_PROGRESS":
			if elapsed >= waitDuration {
				// Perform the actual switchover in mock state
				// This simulates what AWS does: rename source to -old1, promote green to original names
				sourceClusterID := extractClusterIDFromARN(bg.SourceClusterARN)
				s.performSwitchoverLocked(sourceClusterID, bg.TargetEngineVersion, now)

				// Update switchover details
				for i := range bg.SwitchoverDetails {
					bg.SwitchoverDetails[i].Status = "SWITCHOVER_COMPLETED"
				}

				bg.Status = "SWITCHOVER_COMPLETED"
				bg.StatusDetails = "Switchover completed successfully"
				bg.StatusChangedAt = now
			}

		case "DELETING":
			if elapsed >= waitDuration {
				// Remove the Blue-Green deployment
				delete(s.blueGreenDeployments, id)
			}
		}
	}
}
