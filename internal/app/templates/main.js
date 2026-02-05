// Main JavaScript for RDS Maintenance Machine
// This file contains shared functionality for both production and demo modes

// Custom Select Component - using event delegation to handle dynamic content
(function() {
    // Handle trigger clicks via delegation
    document.addEventListener('click', (e) => {
        const trigger = e.target.closest('.custom-select-trigger');
        if (trigger) {
            // Skip if disabled
            if (trigger.disabled || trigger.hasAttribute('disabled')) return;
            
            e.preventDefault();
            e.stopPropagation();
            const select = trigger.closest('.custom-select');
            
            // Close other open selects
            document.querySelectorAll('.custom-select.open').forEach(other => {
                if (other !== select) other.classList.remove('open');
            });
            
            select.classList.toggle('open');
            return;
        }
        
        // Handle option clicks via delegation
        const option = e.target.closest('.custom-select-option');
        if (option) {
            e.stopPropagation();
            const select = option.closest('.custom-select');
            const trigger = select.querySelector('.custom-select-trigger');
            const hiddenSelect = select.querySelector('select');
            const value = option.dataset.value;
            // Use display text if available, otherwise full text
            const displayText = option.dataset.display || option.textContent;
            
            // Update visual state
            select.querySelectorAll('.custom-select-option').forEach(o => o.classList.remove('selected'));
            option.classList.add('selected');
            
            // Update trigger text (use truncated display text)
            trigger.querySelector('span').textContent = displayText;
            trigger.classList.toggle('placeholder', value === '');
            
            // Update hidden select
            hiddenSelect.value = value;
            hiddenSelect.dispatchEvent(new Event('change', { bubbles: true }));
            
            // Close dropdown
            select.classList.remove('open');
            return;
        }
        
        // Close all dropdowns when clicking outside
        if (!e.target.closest('.custom-select')) {
            document.querySelectorAll('.custom-select.open').forEach(select => {
                select.classList.remove('open');
            });
        }
    });
    
    // Keyboard navigation via delegation
    document.addEventListener('keydown', (e) => {
        const trigger = e.target.closest('.custom-select-trigger');
        if (!trigger) return;
        
        const select = trigger.closest('.custom-select');
        const options = select.querySelectorAll('.custom-select-option');
        
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            select.classList.toggle('open');
        } else if (e.key === 'Escape') {
            select.classList.remove('open');
        } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault();
            if (!select.classList.contains('open')) {
                select.classList.add('open');
            }
            const current = select.querySelector('.custom-select-option.highlighted') || 
                           select.querySelector('.custom-select-option.selected');
            const allOptions = Array.from(options);
            let idx = current ? allOptions.indexOf(current) : -1;
            
            if (e.key === 'ArrowDown') {
                idx = Math.min(idx + 1, allOptions.length - 1);
            } else {
                idx = Math.max(idx - 1, 0);
            }
            
            options.forEach(o => o.classList.remove('highlighted'));
            allOptions[idx].classList.add('highlighted');
            allOptions[idx].scrollIntoView({ block: 'nearest' });
        }
    });
})();

// Global state
let selectedOperationId = null;
let pollInterval = null;
let currentRegion = null;
let showErrorsOnly = false;
let currentClusterInfo = null;
let currentBlueGreenDeployment = null; // Active BG deployment for selected cluster
let currentUpgradeTargets = null; // Valid upgrade targets for selected cluster
let currentInstanceTypes = null; // Available instance types for selected cluster
let selectedClusterId = null;
let selectedClusterRegion = null;
let clusterRefreshInterval = null;
let lastStepIndex = -1; // Track step changes for auto-scroll
let lastStepState = null; // Track step state changes for auto-scroll

// Parameter templates for operation types
const paramTemplates = {
    instance_type_change: `<div class="form-group"><label>Target Instance Type</label>
        <div id="instance-type-container">
            <div class="custom-select" data-select-id="param-instance-type">
                <button type="button" class="custom-select-trigger" aria-haspopup="listbox" disabled>
                    <span>Loading instance types...</span>
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
                </button>
                <div class="custom-select-dropdown" role="listbox"></div>
                <select id="param-instance-type" required tabindex="-1" aria-hidden="true"></select>
            </div>
        </div>
    </div>
    <div class="form-group" id="exclude-instances-group" style="display: none;">
        <label>Exclude Instances <span style="font-weight: 400; color: var(--muted-foreground);">(optional)</span></label>
        <div id="exclude-instances-list" class="exclude-instances-list"></div>
        <p class="card-description">Selected instances will be skipped and keep their current configuration.</p>
    </div>
    <div class="form-group">
        <label class="checkbox-label">
            <input type="checkbox" id="param-skip-temp-instance">
            <span>Skip temp instance creation</span>
        </label>
        <p class="card-description">By default, a temporary instance is created for redundancy. Check this to skip temp instance creation (faster but less safe).</p>
    </div>`,
    engine_upgrade: `<div class="form-group"><label>Target Engine Version</label>
        <div id="engine-version-container">
            <div class="custom-select" data-select-id="param-engine-version">
                <button type="button" class="custom-select-trigger" aria-haspopup="listbox" disabled>
                    <span>Loading versions...</span>
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
                </button>
                <div class="custom-select-dropdown" role="listbox"></div>
                <select id="param-engine-version" required tabindex="-1" aria-hidden="true"></select>
            </div>
        </div>
    </div><div class="form-group"><label>Parameter Group (optional)</label><input type="text" id="param-parameter-group" placeholder=""><p class="card-description">Leave blank to auto-detect. Custom parameter settings are automatically migrated to a new parameter group for the target version.</p></div><div style="display: flex; gap: 10px; padding: 10px 12px; border: 1px solid var(--blue); border-radius: 8px; background: var(--blue-muted); margin-top: 12px; margin-bottom: 16px;"><svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; margin-top: 2px;"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg><div style="font-size: 13px; color: var(--text-primary); line-height: 1.4;">This operation uses AWS Blue-Green Deployment for near-zero-downtime upgrades.</div></div>`,
    instance_cycle: `<div style="display: flex; gap: 10px; padding: 10px 12px; border: 1px solid var(--blue); border-radius: 8px; background: var(--blue-muted); margin-bottom: 16px;">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; margin-top: 2px;"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
        <div style="font-size: 13px; color: var(--text-primary); line-height: 1.4;">This operation will reboot all non-autoscaled instances in the cluster one at a time, starting with readers and ending with the writer.</div>
    </div>
    <div class="form-group" id="exclude-instances-group-cycle" style="display: none;">
        <label>Exclude Instances <span style="font-weight: 400; color: var(--muted-foreground);">(optional)</span></label>
        <div id="exclude-instances-list-cycle" class="exclude-instances-list"></div>
        <p class="card-description">Selected instances will be skipped and not rebooted.</p>
    </div>
    <div class="form-group">
        <label class="checkbox-label">
            <input type="checkbox" id="param-skip-temp-instance-cycle">
            <span>Skip temp instance creation</span>
        </label>
        <p class="card-description">By default, a temporary instance is created for redundancy. Check this to skip temp instance creation (faster but less safe).</p>
    </div>`
};

// Alert Dialog functions
function showAlertDialog(message, title = 'Error') {
    const dialog = document.getElementById('alert-dialog');
    const messageEl = document.getElementById('alert-dialog-message');
    const titleEl = dialog.querySelector('.alert-dialog-title');
    
    titleEl.textContent = title;
    messageEl.textContent = message;
    dialog.classList.add('active');
    
    // Focus the OK button for keyboard accessibility
    dialog.querySelector('button').focus();
}

function closeAlertDialog() {
    document.getElementById('alert-dialog').classList.remove('active');
}

// Toast notification for transient errors
function showToast(message, type = 'error', duration = 5000) {
    // Create toast container if it doesn't exist
    let container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        container.style.cssText = 'position: fixed; bottom: 20px; right: 20px; z-index: 1000; display: flex; flex-direction: column; gap: 8px;';
        document.body.appendChild(container);
    }
    
    const toast = document.createElement('div');
    toast.className = 'toast toast-' + type;
    toast.style.cssText = `
        padding: 12px 16px;
        border-radius: 8px;
        font-size: 13px;
        max-width: 400px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
        animation: toastSlideIn 0.2s ease-out;
        display: flex;
        align-items: center;
        gap: 8px;
        ${type === 'error' ? 'background: var(--red-muted); border: 1px solid var(--red); color: var(--foreground);' : ''}
        ${type === 'success' ? 'background: var(--green-muted); border: 1px solid var(--green); color: var(--foreground);' : ''}
        ${type === 'warning' ? 'background: var(--yellow-muted); border: 1px solid var(--yellow); color: var(--foreground);' : ''}
        ${type === 'info' ? 'background: var(--blue-muted); border: 1px solid var(--blue); color: var(--foreground);' : ''}
    `;
    
    // Add icon based on type
    const iconSvg = type === 'error' 
        ? '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>'
        : type === 'success'
        ? '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>'
        : '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--yellow)" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>';
    
    toast.innerHTML = iconSvg + '<span>' + message + '</span>';
    container.appendChild(toast);
    
    // Auto-remove after duration
    setTimeout(() => {
        toast.style.animation = 'toastSlideOut 0.2s ease-in forwards';
        setTimeout(() => toast.remove(), 200);
    }, duration);
    
    // Add animation styles if not present
    if (!document.getElementById('toast-styles')) {
        const style = document.createElement('style');
        style.id = 'toast-styles';
        style.textContent = `
            @keyframes toastSlideIn {
                from { transform: translateX(100%); opacity: 0; }
                to { transform: translateX(0); opacity: 1; }
            }
            @keyframes toastSlideOut {
                from { transform: translateX(0); opacity: 1; }
                to { transform: translateX(100%); opacity: 0; }
            }
        `;
        document.head.appendChild(style);
    }
}

// Format status for display - truncate long "configuring-*" statuses
function formatStatus(status) {
    const fullStatus = status.replace(/ /g, '-');
    let displayStatus = status;
    let tooltip = null;
    
    // Truncate long configuring statuses
    if (status.startsWith('configuring-')) {
        displayStatus = 'configuring';
        tooltip = status;
    } else if (status === 'inaccessible-encryption-credentials' || 
               status === 'inaccessible-encryption-credentials-recoverable') {
        displayStatus = 'inaccessible';
        tooltip = status;
    } else if (status === 'resetting-master-credentials') {
        displayStatus = 'resetting';
        tooltip = status;
    }
    
    const titleAttr = tooltip ? ' title="' + tooltip + '"' : '';
    return '<span class="state-badge state-' + fullStatus + '"' + titleAttr + '>' + displayStatus + '</span>';
}

// Confirm Dialog functions
let confirmDialogResolve = null;

function showConfirmDialog(message, title = 'Confirm', actionLabel = 'Confirm') {
    return new Promise((resolve) => {
        confirmDialogResolve = resolve;
        const dialog = document.getElementById('confirm-dialog');
        const messageEl = document.getElementById('confirm-dialog-message');
        const titleEl = dialog.querySelector('.confirm-dialog-title');
        const actionBtn = document.getElementById('confirm-dialog-action');
        
        titleEl.textContent = title;
        messageEl.textContent = message;
        actionBtn.textContent = actionLabel;
        dialog.classList.add('active');
        
        // Focus the cancel button for safety
        dialog.querySelector('.btn-secondary').focus();
    });
}

function closeConfirmDialog(confirmed) {
    document.getElementById('confirm-dialog').classList.remove('active');
    if (confirmDialogResolve) {
        confirmDialogResolve(confirmed);
        confirmDialogResolve = null;
    }
}

// Close dialogs on Escape key
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        const alertDialog = document.getElementById('alert-dialog');
        if (alertDialog.classList.contains('active')) {
            closeAlertDialog();
        }
        const confirmDialog = document.getElementById('confirm-dialog');
        if (confirmDialog.classList.contains('active')) {
            closeConfirmDialog(false);
        }
    }
});

// Update custom select visually
// Options can have: { value, text, display } where display is optional truncated text for trigger
function updateCustomSelect(selectId, options, selectedValue, placeholder) {
    const container = document.querySelector(`[data-select-id="${selectId}"]`);
    if (!container) return;
    
    const trigger = container.querySelector('.custom-select-trigger');
    const dropdown = container.querySelector('.custom-select-dropdown');
    const hiddenSelect = container.querySelector('select');
    
    // Build options HTML
    let optionsHtml = '';
    let selectedText = placeholder;
    let selectedDisplay = placeholder;
    
    options.forEach(opt => {
        const isSelected = opt.value === selectedValue;
        if (isSelected && opt.value) {
            selectedText = opt.text;
            selectedDisplay = opt.display || opt.text;
        }
        // Store display text in data attribute for use when selecting
        const displayAttr = opt.display ? ` data-display="${opt.display}"` : '';
        // Use html property for rich dropdown content, fallback to text
        const content = opt.html || opt.text;
        optionsHtml += `<div class="custom-select-option${isSelected ? ' selected' : ''}" data-value="${opt.value}"${displayAttr} role="option">${content}</div>`;
    });
    
    // Update dropdown
    dropdown.innerHTML = optionsHtml;
    
    // Update trigger with display text (truncated version)
    trigger.querySelector('span').textContent = selectedDisplay;
    trigger.classList.toggle('placeholder', !selectedValue);
    
    // Update hidden select
    hiddenSelect.innerHTML = options.map(opt => 
        `<option value="${opt.value}"${opt.value === selectedValue ? ' selected' : ''}>${opt.text}</option>`
    ).join('');
}

// Get writer instance from current cluster info
function getWriterInstance() {
    if (!currentClusterInfo || !currentClusterInfo.instances) return null;
    return currentClusterInfo.instances.find(i => i.role === 'writer');
}

// Populate the exclude instances list for instance_type_change
function populateExcludeInstancesList() {
    const container = document.getElementById('exclude-instances-list');
    const group = document.getElementById('exclude-instances-group');
    if (!container || !group) return;
    
    if (!currentClusterInfo || !currentClusterInfo.instances) {
        group.style.display = 'none';
        return;
    }
    
    // Filter to non-autoscaled instances only (autoscaled are already skipped)
    const instances = currentClusterInfo.instances.filter(i => !i.is_auto_scaled);
    
    if (instances.length <= 1) {
        // Only one instance - no point in showing exclusion options
        group.style.display = 'none';
        return;
    }
    
    group.style.display = 'block';
    container.innerHTML = instances.map(inst => {
        const roleClass = inst.role === 'writer' ? 'writer' : '';
        const roleLabel = inst.role === 'writer' ? 'W' : 'R';
        return `<div class="exclude-instance-item">
            <label>
                <input type="checkbox" name="exclude-instance" value="${inst.instance_id}">
                <span class="instance-name">${inst.instance_id}</span>
                <span class="instance-meta">${inst.instance_type}</span>
                <span class="instance-role ${roleClass}">${roleLabel}</span>
            </label>
        </div>`;
    }).join('');
}

// Get list of excluded instance IDs from checkboxes
function getExcludedInstances(suffix = '') {
    const checkboxes = document.querySelectorAll('input[name="exclude-instance' + suffix + '"]:checked');
    return Array.from(checkboxes).map(cb => cb.value);
}

// Populate the exclude instances list for instance_cycle
function populateExcludeInstancesListCycle() {
    const container = document.getElementById('exclude-instances-list-cycle');
    const group = document.getElementById('exclude-instances-group-cycle');
    if (!container || !group) return;
    
    if (!currentClusterInfo || !currentClusterInfo.instances) {
        group.style.display = 'none';
        return;
    }
    
    // Filter to non-autoscaled instances only (autoscaled are already skipped)
    const instances = currentClusterInfo.instances.filter(i => !i.is_auto_scaled);
    
    if (instances.length <= 1) {
        // Only one instance - no point in showing exclusion options
        group.style.display = 'none';
        return;
    }
    
    group.style.display = 'block';
    container.innerHTML = instances.map(inst => {
        const roleClass = inst.role === 'writer' ? 'writer' : '';
        const roleLabel = inst.role === 'writer' ? 'W' : 'R';
        return `<div class="exclude-instance-item">
            <label>
                <input type="checkbox" name="exclude-instance-cycle" value="${inst.instance_id}">
                <span class="instance-name">${inst.instance_id}</span>
                <span class="instance-meta">${inst.instance_type}</span>
                <span class="instance-role ${roleClass}">${roleLabel}</span>
            </label>
        </div>`;
    }).join('');
}

// Render operation parameters with auto-population
function renderParams(opType) {
    const container = document.getElementById('params-container');
    container.innerHTML = paramTemplates[opType] || '';
    
    // Auto-populate based on current primary
    if (opType === 'instance_type_change') {
        const selectContainer = document.querySelector('[data-select-id="param-instance-type"]');
        const writer = getWriterInstance();
        const currentType = writer?.instance_type || '';
        
        if (currentInstanceTypes && currentInstanceTypes.instance_types) {
            const instanceTypes = currentInstanceTypes.instance_types;
            
            if (instanceTypes.length === 0) {
                // No instance types available
                const trigger = selectContainer?.querySelector('.custom-select-trigger');
                if (trigger) {
                    trigger.querySelector('span').textContent = 'No instance types available';
                    trigger.disabled = true;
                }
            } else {
                // Group instance types by family (e.g., r6g, r5, t3)
                const families = {};
                instanceTypes.forEach(t => {
                    // Parse instance class: db.<family>.<size>
                    const parts = t.instance_class.replace('db.', '').split('.');
                    const family = parts[0] || 'other';
                    if (!families[family]) families[family] = [];
                    families[family].push(t);
                });
                
                // Build dropdown with groups
                const dropdown = selectContainer.querySelector('.custom-select-dropdown');
                const select = document.getElementById('param-instance-type');
                
                dropdown.innerHTML = '';
                select.innerHTML = '';
                
                // Sort families: by type priority (r, x, m, t), then by generation (newest first)
                const typePriority = { r: 0, x: 1, m: 2, t: 3 };
                const sortedFamilies = Object.keys(families).sort((a, b) => {
                    // Extract type letter and generation (e.g., "r6g" -> type="r", gen="6g")
                    const typeA = a.charAt(0);
                    const typeB = b.charAt(0);
                    const genA = a.substring(1);
                    const genB = b.substring(1);
                    
                    // First sort by type priority
                    const prioA = typePriority[typeA] ?? 99;
                    const prioB = typePriority[typeB] ?? 99;
                    if (prioA !== prioB) return prioA - prioB;
                    
                    // Then sort by generation (descending - newer/higher first)
                    // Extract numeric part for comparison (e.g., "6g" -> 6, "6gd" -> 6)
                    const numA = parseInt(genA) || 0;
                    const numB = parseInt(genB) || 0;
                    if (numA !== numB) return numB - numA;
                    
                    // Finally sort alphabetically for same generation (e.g., r6g before r6gd, r6i)
                    return a.localeCompare(b);
                });
                
                // Size order for sorting within families
                const sizeOrder = ['micro', 'small', 'medium', 'large', 'xlarge', '2xlarge', '4xlarge', '8xlarge', '12xlarge', '16xlarge', '24xlarge', '32xlarge', '48xlarge'];
                
                let isFirst = true;
                sortedFamilies.forEach(family => {
                    const types = families[family];
                    
                    // Sort types by size within family
                    types.sort((a, b) => {
                        const sizeA = a.instance_class.replace('db.', '').split('.')[1] || '';
                        const sizeB = b.instance_class.replace('db.', '').split('.')[1] || '';
                        const orderA = sizeOrder.indexOf(sizeA);
                        const orderB = sizeOrder.indexOf(sizeB);
                        if (orderA === -1 && orderB === -1) return sizeA.localeCompare(sizeB);
                        if (orderA === -1) return 1;
                        if (orderB === -1) return -1;
                        return orderA - orderB;
                    });
                    
                    // Check if this is an NVMe family (ends with 'd', e.g., r6gd, r5d)
                    const isNvmeFamily = /d$/.test(family);
                    
                    // Add family header
                    const header = document.createElement('div');
                    header.className = 'custom-select-group-header';
                    header.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;' + 
                        (!isFirst ? ' margin-top: 8px; border-top: 1px solid var(--border); padding-top: 12px;' : '');
                    if (isNvmeFamily) {
                        header.innerHTML = family.toUpperCase() + ' <span style="text-transform: none; font-weight: 500; color: var(--blue);">(read optimized)</span>';
                    } else {
                        header.textContent = family.toUpperCase();
                    }
                    dropdown.appendChild(header);
                    isFirst = false;
                    
                    types.forEach(t => {
                        const isCurrent = t.instance_class === currentType;
                        
                        const opt = document.createElement('div');
                        opt.className = 'custom-select-option' + (isCurrent ? ' current-indicator' : '');
                        opt.dataset.value = t.instance_class;
                        opt.setAttribute('role', 'option');
                        
                        // Build label with current indicator if applicable
                        if (isCurrent) {
                            opt.innerHTML = t.instance_class + '<span style="font-size: 11px; color: var(--blue); margin-left: 8px;">(current)</span>';
                        } else {
                            opt.textContent = t.instance_class;
                        }
                        dropdown.appendChild(opt);
                        
                        const selectOpt = document.createElement('option');
                        selectOpt.value = t.instance_class;
                        selectOpt.textContent = t.instance_class;
                        select.appendChild(selectOpt);
                    });
                });
                
                // Enable trigger and set placeholder
                const trigger = selectContainer.querySelector('.custom-select-trigger');
                trigger.disabled = false;
                trigger.removeAttribute('disabled');
                trigger.querySelector('span').textContent = currentType ? `Current: ${currentType}` : 'Select instance type...';
            }
        } else if (currentInstanceTypes && currentInstanceTypes.error) {
            // API error - show error state
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Failed to load types';
            }
        } else if (currentClusterInfo) {
            // Cluster is selected but instance types not loaded yet
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Loading instance types...';
            }
        } else {
            // No cluster selected
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Select a cluster first...';
            }
        }
        
        // Populate exclusion list with cluster instances
        populateExcludeInstancesList();
    } else if (opType === 'engine_upgrade') {
        const container = document.getElementById('params-container');
        const selectContainer = document.querySelector('[data-select-id="param-engine-version"]');
        const paramGroupInput = document.getElementById('param-parameter-group');
        
        // Check if there's an active Blue-Green deployment to adopt
        if (currentBlueGreenDeployment) {
            // Disable parameter group field (can't change for existing deployment)
            if (paramGroupInput) {
                paramGroupInput.disabled = true;
                paramGroupInput.style.backgroundColor = 'var(--bg-tertiary)';
                paramGroupInput.style.cursor = 'not-allowed';
                paramGroupInput.placeholder = 'Using existing deployment settings';
            }
            
            // Set up dropdown with just the target version, disabled
            if (selectContainer && currentBlueGreenDeployment.target_engine_version) {
                const version = currentBlueGreenDeployment.target_engine_version;
                const options = [{ value: version, text: version }];
                updateCustomSelect('param-engine-version', options, version, version);
                
                // Disable the dropdown
                const trigger = selectContainer.querySelector('.custom-select-trigger');
                if (trigger) {
                    trigger.disabled = true;
                    trigger.style.backgroundColor = 'var(--bg-tertiary)';
                    trigger.style.cursor = 'not-allowed';
                }
            }
            
            // Add notice about adopting existing deployment
            const notice = document.createElement('div');
            notice.className = 'existing-bg-notice';
            notice.style.cssText = 'margin-bottom: 16px; padding: 12px; background: var(--yellow-muted); border-radius: 6px; border-left: 3px solid var(--yellow);';
            notice.innerHTML = `<strong>Existing Blue-Green Deployment Detected</strong><br>` +
                `<span style="font-size: 12px; color: var(--text-secondary);">` +
                `Status: ${currentBlueGreenDeployment.status} | ` +
                `This operation will adopt the existing deployment instead of creating a new one.</span>`;
            container.insertBefore(notice, container.firstChild);
        } else if (currentUpgradeTargets && currentUpgradeTargets.upgrade_targets) {
            // Populate dropdown with valid upgrade targets
            const targets = currentUpgradeTargets.upgrade_targets;
            
            if (targets.length === 0) {
                // No upgrades available
                const options = [{ value: '', text: 'No upgrades available' }];
                updateCustomSelect('param-engine-version', options, '', 'No upgrades available');
                
                const trigger = selectContainer.querySelector('.custom-select-trigger');
                if (trigger) {
                    trigger.disabled = true;
                }
            } else {
                // Group by major/minor upgrades
                const minorUpgrades = targets.filter(t => !t.is_major_version_upgrade);
                const majorUpgrades = targets.filter(t => t.is_major_version_upgrade);
                
                // Build options with grouping
                const dropdown = selectContainer.querySelector('.custom-select-dropdown');
                const select = document.getElementById('param-engine-version');
                
                dropdown.innerHTML = '';
                select.innerHTML = '';
                
                // Add minor upgrades
                if (minorUpgrades.length > 0) {
                    const minorHeader = document.createElement('div');
                    minorHeader.className = 'custom-select-group-header';
                    minorHeader.textContent = 'Minor Upgrades';
                    minorHeader.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;';
                    dropdown.appendChild(minorHeader);
                    
                    minorUpgrades.forEach(t => {
                        const opt = document.createElement('div');
                        opt.className = 'custom-select-option';
                        opt.dataset.value = t.engine_version;
                        opt.setAttribute('role', 'option');
                        opt.textContent = t.engine_version;
                        dropdown.appendChild(opt);
                        
                        const selectOpt = document.createElement('option');
                        selectOpt.value = t.engine_version;
                        selectOpt.textContent = t.engine_version;
                        select.appendChild(selectOpt);
                    });
                }
                
                // Add major upgrades
                if (majorUpgrades.length > 0) {
                    const majorHeader = document.createElement('div');
                    majorHeader.className = 'custom-select-group-header';
                    majorHeader.textContent = 'Major Upgrades';
                    majorHeader.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;' + (minorUpgrades.length > 0 ? ' margin-top: 8px; border-top: 1px solid var(--border); padding-top: 12px;' : '');
                    dropdown.appendChild(majorHeader);
                    
                    majorUpgrades.forEach(t => {
                        const opt = document.createElement('div');
                        opt.className = 'custom-select-option';
                        opt.dataset.value = t.engine_version;
                        opt.setAttribute('role', 'option');
                        opt.textContent = t.engine_version;
                        dropdown.appendChild(opt);
                        
                        const selectOpt = document.createElement('option');
                        selectOpt.value = t.engine_version;
                        selectOpt.textContent = t.engine_version;
                        select.appendChild(selectOpt);
                    });
                }
                
                // Enable trigger
                const trigger = selectContainer.querySelector('.custom-select-trigger');
                trigger.disabled = false;
                trigger.removeAttribute('disabled');
                trigger.querySelector('span').textContent = 'Select version...';
            }
        } else if (currentUpgradeTargets && currentUpgradeTargets.error) {
            // API error - show error state
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Failed to load versions';
            }
        } else if (currentClusterInfo) {
            // Cluster is selected but upgrade targets not loaded yet - show loading
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Loading versions...';
            }
        } else {
            // No cluster selected
            const trigger = selectContainer?.querySelector('.custom-select-trigger');
            if (trigger) {
                trigger.querySelector('span').textContent = 'Select a cluster first...';
            }
        }
    } else if (opType === 'instance_cycle') {
        // Populate exclusion list with cluster instances
        populateExcludeInstancesListCycle();
    }
}

// Load regions (production mode only)
async function loadRegions() {
    try {
        const response = await fetch('/api/regions');
        const data = await response.json();
        
        const options = [
            { value: '', text: 'Select region...' },
            ...data.regions.map(r => ({ value: r, text: r }))
        ];
        
        updateCustomSelect('region-select', options, data.default_region, 'Select region...');
        
        // Enable region selector
        const trigger = document.querySelector('[data-select-id="region-select"] .custom-select-trigger');
        if (trigger) {
            trigger.disabled = false;
            trigger.removeAttribute('disabled');
        }
        
        // If default region is set, load clusters for it
        if (data.default_region) {
            currentRegion = data.default_region;
            loadClusters(data.default_region);
        }
    } catch (err) {
        console.error('failed to load regions:', err);
        updateCustomSelect('region-select', [{ value: '', text: 'failed to load regions' }], '', 'failed to load regions');
        showToast('Failed to load AWS regions: ' + err.message, 'error');
    }
}

// Truncate text with ellipsis
function truncateText(text, maxLength) {
    if (text.length <= maxLength) return text;
    return text.substring(0, maxLength - 3) + '...';
}

// Load clusters for selected region (production mode)
async function loadClusters(region) {
    const trigger = document.querySelector('[data-select-id="cluster-select"] .custom-select-trigger');
    const loading = document.getElementById('cluster-loading');
    
    if (trigger) {
        trigger.disabled = true;
        trigger.setAttribute('disabled', '');
    }
    updateCustomSelect('cluster-select', [{ value: '', text: 'Loading clusters...' }], '', 'Loading clusters...');
    if (loading) loading.style.display = 'block';
    
    try {
        const response = await fetch('/api/regions/' + region + '/clusters');
        const clusters = await response.json();
        
        if (clusters.length === 0) {
            updateCustomSelect('cluster-select', [{ value: '', text: 'No Aurora clusters found' }], '', 'No Aurora clusters found');
        } else {
            const options = [
                { value: '', text: 'Select cluster...' },
                ...clusters.map(c => {
                    const engineInfo = `${c.engine} ${c.engine_version}`;
                    return { 
                        value: c.cluster_id, 
                        text: c.cluster_id,
                        display: truncateText(c.cluster_id, 40),
                        html: `<div style="display: flex; flex-direction: column; gap: 2px;"><span style="white-space: nowrap;">${c.cluster_id}</span><span style="font-size: 12px; color: var(--muted-foreground);">${engineInfo}</span></div>`
                    };
                })
            ];
            updateCustomSelect('cluster-select', options, '', 'Select cluster...');
            if (trigger) {
                trigger.disabled = false;
                trigger.removeAttribute('disabled');
            }
        }
    } catch (err) {
        console.error('failed to load clusters:', err);
        updateCustomSelect('cluster-select', [{ value: '', text: 'failed to load clusters' }], '', 'failed to load clusters');
        showToast('Failed to load clusters: ' + err.message, 'error');
    } finally {
        if (loading) loading.style.display = 'none';
    }
}

// Enable or disable operation type dropdown
function setOperationTypeEnabled(enabled) {
    const container = document.querySelector('[data-select-id="operation-type"]');
    if (!container) return;
    const trigger = container.querySelector('.custom-select-trigger');
    if (!trigger) return;
    
    if (enabled) {
        trigger.disabled = false;
        trigger.removeAttribute('disabled');
        trigger.querySelector('span').textContent = 'Select operation...';
    } else {
        trigger.disabled = true;
        trigger.setAttribute('disabled', '');
        trigger.querySelector('span').textContent = 'Select a cluster first...';
        // Also clear the hidden select
        const select = document.getElementById('operation-type');
        if (select) select.value = '';
        // Clear params container
        const paramsContainer = document.getElementById('params-container');
        if (paramsContainer) paramsContainer.innerHTML = '';
    }
}

// Fetch cluster info for auto-population
async function fetchClusterInfo(clusterId, region) {
    currentClusterInfo = null;
    currentBlueGreenDeployment = null;
    currentUpgradeTargets = null;
    currentInstanceTypes = null;
    
    if (!clusterId) {
        setOperationTypeEnabled(false);
        return;
    }
    
    try {
        const headers = { 'X-Cluster-Id': clusterId };
        if (region) headers['X-Region'] = region;
        
        // Fetch cluster info, blue-green deployments, upgrade targets, and instance types in parallel
        const [clusterResponse, bgResponse, upgradeResponse, instanceTypesResponse] = await Promise.all([
            fetch('/api/cluster', { headers }),
            fetch('/api/cluster/blue-green', { headers }),
            fetch('/api/cluster/upgrade-targets', { headers }),
            fetch('/api/cluster/instance-types', { headers })
        ]);
        
        if (clusterResponse.ok) {
            currentClusterInfo = await clusterResponse.json();
        }
        
        // Check for active blue-green deployment
        if (bgResponse.ok) {
            const bgDeployments = await bgResponse.json();
            // Find an active deployment (PROVISIONING or AVAILABLE)
            if (Array.isArray(bgDeployments)) {
                currentBlueGreenDeployment = bgDeployments.find(
                    bg => bg.status === 'PROVISIONING' || bg.status === 'AVAILABLE'
                ) || null;
            }
        }
        
        // Get valid upgrade targets
        if (upgradeResponse.ok) {
            currentUpgradeTargets = await upgradeResponse.json();
        } else {
            console.error('failed to fetch upgrade targets:', upgradeResponse.status, await upgradeResponse.text());
            // Set empty upgrade targets to distinguish from "not loaded yet"
            currentUpgradeTargets = { upgrade_targets: [], error: true };
        }
        
        // Get available instance types
        if (instanceTypesResponse.ok) {
            currentInstanceTypes = await instanceTypesResponse.json();
        } else {
            console.error('failed to fetch instance types:', instanceTypesResponse.status, await instanceTypesResponse.text());
            currentInstanceTypes = { instance_types: [], error: true };
        }
        
        // Enable operation type dropdown now that cluster is selected
        setOperationTypeEnabled(true);
        
        // Re-render params if operation type is already selected
        const opType = document.getElementById('operation-type').value;
        if (opType) {
            renderParams(opType);
        }
    } catch (err) {
        console.error('failed to fetch cluster info:', err);
        showToast('Failed to load cluster info: ' + err.message, 'error');
    }
}

// Update cluster status panel (production mode)
function updateClusterStatus(clusterId, region) {
    selectedClusterId = clusterId;
    selectedClusterRegion = region;
    loadClusterStatus();
}

// Load cluster status from AWS API (production mode)
// If cluster is not found, retries for up to 10 seconds before showing not found
async function loadClusterStatus(retryUntil = null) {
    const card = document.getElementById('ops-clusters-card');
    if (!card) return;
    
    if (!selectedClusterId) {
        card.style.display = 'none';
        return;
    }
    card.style.display = 'block';
    
    const container = document.getElementById('ops-clusters-container');
    
    try {
        const headers = { 'X-Cluster-Id': selectedClusterId };
        if (selectedClusterRegion) headers['X-Region'] = selectedClusterRegion;
        
        // Fetch cluster info and Blue-Green deployments in parallel
        const [clusterResponse, bgResponse] = await Promise.all([
            fetch('/api/cluster', { headers }),
            fetch('/api/cluster/blue-green', { headers })
        ]);
        
        if (clusterResponse.ok) {
            const clusterInfo = await clusterResponse.json();
            const bgDeployments = bgResponse.ok ? await bgResponse.json() : [];
            renderClusterStatus(clusterInfo, bgDeployments);
            
            // Update refresh indicator
            const indicator = document.getElementById('cluster-refresh-indicator');
            if (indicator) {
                indicator.textContent = new Date().toLocaleTimeString();
            }
        } else if (clusterResponse.status === 404 || clusterResponse.status === 500) {
            // Cluster not found - retry for 10 seconds before showing not found
            const now = Date.now();
            if (retryUntil === null) {
                // First attempt - start the retry period
                retryUntil = now + 10000; // 10 seconds from now
            }
            
            if (now < retryUntil) {
                // Still within retry period - show loading state and retry
                const remainingSeconds = Math.ceil((retryUntil - now) / 1000);
                if (container) {
                    container.innerHTML = `<div class="empty-state" style="padding: 24px; color: var(--text-secondary);">Looking for cluster... (${remainingSeconds}s)</div>`;
                }
                // Retry after 2 seconds
                setTimeout(() => loadClusterStatus(retryUntil), 2000);
            } else {
                // Retry period expired - show not found with retry button
                if (container) {
                    container.innerHTML = `<div class="empty-state" style="padding: 24px;">
                        <span style="color: var(--text-secondary);">Cluster not found</span>
                        <button onclick="loadClusterStatus()" style="margin-left: 12px; background: none; border: none; cursor: pointer; font-size: 16px; padding: 4px 8px;" title="Retry">&#x1F504;</button>
                    </div>`;
                }
            }
        }
    } catch (err) {
        console.error('failed to load cluster status:', err);
        if (container) {
            container.innerHTML = `<div class="empty-state" style="padding: 24px;">
                <span style="color: var(--text-secondary);">Failed to load cluster status</span>
                <button onclick="loadClusterStatus()" style="margin-left: 12px; background: none; border: none; cursor: pointer; font-size: 16px; padding: 4px 8px;" title="Retry">&#x1F504;</button>
            </div>`;
        }
    }
}

// Render cluster status panel (production mode)
function renderClusterStatus(cluster, blueGreenDeployments) {
    const container = document.getElementById('ops-clusters-container');
    if (!container || !cluster) return;
    
    // Sort instances: writer first, then by ID
    const instances = (cluster.instances || []).sort((a, b) => {
        if (a.role === 'writer' && b.role !== 'writer') return -1;
        if (a.role !== 'writer' && b.role === 'writer') return 1;
        return a.instance_id.localeCompare(b.instance_id);
    });
    
    // Build Blue-Green deployment section if any exist
    let bgHtml = '';
    if (blueGreenDeployments && blueGreenDeployments.length > 0) {
        bgHtml = '<div class="separator"></div>' +
            '<div style="font-size: 11px; color: var(--muted-foreground); margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.05em;">Blue-Green Deployments</div>' +
            blueGreenDeployments.map(bg => {
                const isSource = bg.source && bg.source.includes(cluster.cluster_id);
                const roleLabel = isSource ? 'BLUE (Source)' : 'GREEN (Target)';
                const statusClass = (bg.status || '').toLowerCase().replace(/_/g, '-');
                return '<div class="bg-deployment-item">' +
                    '<div class="bg-deployment-header">' +
                        '<span class="bg-deployment-name">' + (bg.name || bg.identifier) + '</span>' +
                        '<span class="bg-role-badge ' + (isSource ? 'blue' : 'green') + '">' + roleLabel + '</span>' +
                    '</div>' +
                    '<div class="bg-deployment-status">' +
                        '<span class="state-badge state-' + statusClass + '">' + (bg.status || '').replace(/_/g, ' ') + '</span>' +
                    '</div>' +
                    (bg.tasks && bg.tasks.length > 0 ? 
                        '<div class="bg-tasks">' +
                            bg.tasks.map(task => {
                                const taskStatusClass = (task.status || '').toLowerCase().replace(/_/g, '-');
                                return '<div class="bg-task">' +
                                    '<span class="bg-task-name">' + (task.name || '').replace(/_/g, ' ') + '</span>' +
                                    '<span class="bg-task-status state-badge state-' + taskStatusClass + '">' + (task.status || '') + '</span>' +
                                '</div>';
                            }).join('') +
                        '</div>' : '') +
                '</div>';
            }).join('');
    }
    
    container.innerHTML = 
        '<div class="cluster-card" style="margin: 0;">' +
            '<div class="cluster-header">' +
                '<span class="cluster-name">' + cluster.cluster_id + '</span>' +
                '<span class="state-badge state-' + cluster.status + '">' + cluster.status + '</span>' +
            '</div>' +
            '<div class="cluster-info">' + cluster.engine + ' ' + cluster.engine_version + '</div>' +
            '<table class="data-table cols-5-storage">' +
                '<thead>' +
                    '<tr>' +
                        '<th>Instance</th>' +
                        '<th>Role</th>' +
                        '<th>Type</th>' +
                        '<th>Storage</th>' +
                        '<th>Status</th>' +
                    '</tr>' +
                '</thead>' +
                '<tbody>' +
                    instances.map(inst => {
                        const roleClass = inst.role === 'writer' ? 'writer' : (inst.is_auto_scaled ? 'autoscaled' : '');
                        const roleLabel = inst.role === 'writer' ? 'W' : (inst.is_auto_scaled ? 'A' : 'R');
                        let storageInfo = inst.storage_type || '-';
                        if (inst.iops) storageInfo += ' (' + inst.iops + ' IOPS)';
                        return '<tr>' +
                            '<td style="font-weight: 500;">' + inst.instance_id + '</td>' +
                            '<td><span class="instance-role ' + roleClass + '">' + roleLabel + '</span></td>' +
                            '<td style="color: var(--muted-foreground);">' + inst.instance_type + '</td>' +
                            '<td style="color: var(--muted-foreground);">' + storageInfo + '</td>' +
                            '<td>' + formatStatus(inst.status) + '</td>' +
                        '</tr>';
                    }).join('') +
                '</tbody>' +
            '</table>' +
            bgHtml +
        '</div>';
}

// Start cluster status auto-refresh (production mode)
function startClusterStatusRefresh() {
    if (clusterRefreshInterval) {
        clearInterval(clusterRefreshInterval);
    }
    // Production mode uses slower refresh (5 seconds) to avoid API rate limits
    clusterRefreshInterval = setInterval(loadClusterStatus, 5000);
}

// Load operations list
async function loadOperations() {
    try {
        const response = await fetch('/api/operations');
        const operations = await response.json();
        renderOperationsList(operations);
    } catch (err) {
        console.error('failed to load operations:', err);
    }
}

// Render operations list
function renderOperationsList(operations) {
    const list = document.getElementById('operations-list');
    if (!operations || operations.length === 0) {
        list.innerHTML = '<div class="empty-state"><p>No operations yet</p></div>';
        return;
    }
    
    operations.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
    
    list.innerHTML = operations.map(op => {
        const typeName = {
            instance_type_change: 'Instance Type Change',
            storage_type_change: 'Storage Type Change',
            engine_upgrade: 'Engine Upgrade',
            instance_cycle: 'Instance Cycle'
        }[op.type] || op.type;
        
        return '<div class="operation-item ' + (op.id === selectedOperationId ? 'selected' : '') + '" onclick="selectOperation(\'' + op.id + '\')">' +
            '<div class="operation-type">' + typeName + '</div>' +
            '<div class="operation-cluster">' + op.cluster_id + (op.region ? ' <span style="color: var(--muted-foreground); font-size: 11px;">(' + op.region + ')</span>' : '') + '</div>' +
            '<div class="operation-meta">' +
                '<span class="state-badge state-' + op.state + '">' + op.state.replace('_', ' ') + '</span>' +
                '<span>' + new Date(op.created_at).toLocaleString() + '</span>' +
            '</div>' +
        '</div>';
    }).join('');
}

// Select an operation
async function selectOperation(id) {
    selectedOperationId = id;
    // Reset step tracking for new operation
    lastStepIndex = -1;
    lastStepState = null;
    
    loadOperations();
    loadOperationDetail(id);
    
    if (pollInterval) clearInterval(pollInterval);
    // Demo mode uses faster polling
    const pollMs = typeof isDemoMode !== 'undefined' && isDemoMode ? 1000 : 3000;
    pollInterval = setInterval(() => loadOperationDetail(id), pollMs);
}

// Load operation detail
async function loadOperationDetail(id) {
    try {
        const [opResponse, eventsResponse] = await Promise.all([
            fetch('/api/operations/' + id),
            fetch('/api/operations/' + id + '/events')
        ]);
        
        const op = await opResponse.json();
        const events = await eventsResponse.json();
        
        // Update cluster status panel (same API for both modes)
        updateClusterStatus(op.cluster_id, op.region);
        
        renderOperationDetail(op, events);
    } catch (err) {
        console.error('failed to load operation details:', err);
    }
}

// Format timeout for display
function formatTimeout(seconds) {
    if (!seconds) return 'default (45 min)';
    const mins = Math.floor(seconds / 60);
    if (mins >= 60) {
        const hrs = Math.floor(mins / 60);
        const remMins = mins % 60;
        return hrs + 'h ' + (remMins > 0 ? remMins + 'm' : '');
    }
    return mins + ' min';
}

// Format duration in human-readable format
function formatDuration(startTime, endTime) {
    if (!startTime) return '-';
    const start = new Date(startTime);
    const end = endTime ? new Date(endTime) : new Date();
    const diffMs = end - start;
    
    if (diffMs < 0) return '-';
    
    const seconds = Math.floor(diffMs / 1000);
    if (seconds < 60) return seconds + 's';
    
    const minutes = Math.floor(seconds / 60);
    const remSeconds = seconds % 60;
    if (minutes < 60) return minutes + 'm ' + remSeconds + 's';
    
    const hours = Math.floor(minutes / 60);
    const remMinutes = minutes % 60;
    return hours + 'h ' + remMinutes + 'm ' + remSeconds + 's';
}

// Get duration for a step
function getStepDuration(step) {
    return formatDuration(step.started_at, step.completed_at);
}

// Toggle errors only filter
function toggleErrorsOnly(checked) {
    showErrorsOnly = checked;
    if (selectedOperationId) {
        loadOperationDetail(selectedOperationId);
    }
}

// Render operation detail
function renderOperationDetail(op, events) {
    const container = document.getElementById('operation-detail');
    const typeName = {
        instance_type_change: 'Instance Type Change',
        storage_type_change: 'Storage Type Change',
        engine_upgrade: 'Engine Upgrade',
        instance_cycle: 'Instance Cycle'
    }[op.type] || op.type;
    
    let buttons = '';
    if (op.state === 'created') {
        buttons = '<button onclick="startOperation(\'' + op.id + '\')" class="btn-success">Start Operation</button>' +
                  '<button onclick="deleteOperation(\'' + op.id + '\')" class="btn-danger" style="margin-left: 8px;">Delete</button>';
    } else if (op.state === 'paused') {
        buttons = '<button onclick="showResumeModal()" class="btn-primary">Resume / Rollback / Abort</button>';
    } else if (op.state === 'running') {
        buttons = '<button onclick="pauseOperation(\'' + op.id + '\')" class="btn-warning">Pause</button>';
    }
    
    // Allow timeout edit for non-terminal states (production mode only)
    const canEditTimeout = !isDemoMode && ['created', 'running', 'paused'].includes(op.state);
    
    // Preserve scroll positions
    const existingEventsList = container.querySelector('.events-list');
    const eventsScrollTop = existingEventsList ? existingEventsList.scrollTop : 0;
    const existingStepsList = container.querySelector('.steps-list');
    const stepsScrollTop = existingStepsList ? existingStepsList.scrollTop : 0;
    
    // Ensure steps is an array
    const steps = op.steps || [];
    
    // Detect if we need to auto-scroll to a step
    const currentStepIndex = op.current_step_index || 0;
    const currentStep = steps[currentStepIndex];
    const currentStepState = currentStep?.state || null;
    const stepChanged = currentStepIndex !== lastStepIndex;
    const stepFailed = currentStepState === 'failed' && lastStepState !== 'failed';
    const shouldAutoScroll = stepChanged || stepFailed;
    
    // Update tracking variables
    lastStepIndex = currentStepIndex;
    lastStepState = currentStepState;
    
    // Filter events if showing errors only
    let filteredEvents = (events || []).slice().reverse();
    if (showErrorsOnly) {
        filteredEvents = filteredEvents.filter(e => 
            e.type === 'error' || e.type === 'step_failed' || e.type === 'operation_failed' ||
            (e.message && e.message.toLowerCase().includes('error'))
        );
    }
    
    // Build timeout info row (only in production mode)
    let timeoutRow = '';
    if (!isDemoMode) {
        timeoutRow = '<div class="info-item" style="position: relative;">' +
            '<div class="info-label">Wait Timeout</div>' +
            '<div class="info-value">' + formatTimeout(op.wait_timeout) + '</div>' +
            (canEditTimeout ? '<button onclick="showTimeoutModal(' + (op.wait_timeout || 2700) + ')" class="btn-copy" title="Edit Timeout" style="position: absolute; bottom: 12px; right: 10px; padding: 2px;"><svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/></svg></button>' : '') +
        '</div>' +
        '<div class="info-item"><div class="info-label">Region</div><div class="info-value">' + (op.region || '-') + '</div></div>';
    }
    
    container.innerHTML = 
        '<div class="card-header">' +
            '<div>' +
                '<span class="card-title" style="font-size: 16px;">' + typeName + '</span>' +
                '<p class="card-description">' + op.cluster_id + (!isDemoMode && op.region ? ' (' + op.region + ')' : '') + '</p>' +
            '</div>' +
            '<span class="state-badge state-' + op.state + '">' + op.state.replace('_', ' ') + '</span>' +
        '</div>' +
        
        '<div class="detail-section">' +
            '<h3>Information</h3>' +
            '<div class="info-grid">' +
                '<div class="info-item" style="position: relative;"><div class="info-label">Operation ID</div><div class="info-value" style="font-family: monospace; font-size: 12px;">' + op.id + '</div><button onclick="copyToClipboard(\'' + op.id + '\')" class="btn-copy" title="Copy ID" style="position: absolute; bottom: 12px; right: 10px; padding: 2px;"><svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg></button></div>' +
                '<div class="info-item"><div class="info-label">Created</div><div class="info-value">' + new Date(op.created_at).toLocaleString() + '</div></div>' +
                '<div class="info-item"><div class="info-label">Progress</div><div class="info-value">' + op.current_step_index + ' / ' + steps.length + ' steps</div></div>' +
                '<div class="info-item"><div class="info-label">Duration</div><div class="info-value">' + formatDuration(op.started_at, op.completed_at) + '</div></div>' +
                timeoutRow +
            '</div>' +
            (op.pause_reason ? '<div class="info-item" style="margin-top: 12px; border-color: var(--yellow); background: var(--yellow-muted);"><div class="info-label">Pause Reason</div><div class="info-value">' + op.pause_reason + '</div></div>' : '') +
            (op.error ? '<div class="info-item" style="margin-top: 12px; border-color: var(--red); background: var(--red-muted);"><div class="info-label">Error</div><div class="info-value">' + op.error + '</div></div>' : '') +
        '</div>' +
        
        '<div class="detail-section">' +
            '<h3>Steps</h3>' +
            '<div class="steps-list">' +
                (steps.length === 0 ? '<div class="empty-state" style="padding: 24px;">No steps yet (start operation to generate steps)</div>' :
                steps.map((step, i) => {
                    let numClass = '';
                    let showError = step.state === 'failed' && step.error;
                    const isCurrent = i === op.current_step_index && op.state === 'running';
                    const isFailed = step.state === 'failed';
                    if (step.state === 'completed') numClass = 'completed';
                    else if (isFailed) numClass = 'failed';
                    else if (isCurrent) numClass = 'current';
                    
                    const stepDuration = step.started_at ? getStepDuration(step) : '';
                    return '<div class="step-item" data-step-index="' + i + '"' + (isCurrent ? ' data-current="true"' : '') + (isFailed ? ' data-failed="true"' : '') + '>' +
                        '<div class="step-number ' + numClass + '">' + (i + 1) + '</div>' +
                        '<div class="step-content">' +
                            '<div class="step-name">' + step.name + (stepDuration ? '<span class="step-duration">' + stepDuration + '</span>' : '') + '</div>' +
                            '<div class="step-desc">' + step.description + '</div>' +
                            (step.wait_condition ? '<div class="step-status">' + step.wait_condition + '</div>' : '') +
                            (showError ? '<div class="step-status" style="color: var(--red);">Error: ' + step.error + '</div>' : '') +
                        '</div>' +
                    '</div>';
                }).join('')) +
            '</div>' +
        '</div>' +
        
        '<div class="detail-section">' +
            '<div class="events-header">' +
                '<h3>Events</h3>' +
                '<label class="events-filter">' +
                    '<input type="checkbox" ' + (showErrorsOnly ? 'checked' : '') + ' onchange="toggleErrorsOnly(this.checked)">' +
                    '<span>Errors only</span>' +
                '</label>' +
            '</div>' +
            '<div class="events-list">' +
                (filteredEvents.length === 0 ? '<div class="empty-state" style="padding: 24px;">' + (showErrorsOnly ? 'No errors' : 'No events yet') + '</div>' :
                filteredEvents.slice(0, 50).map(e => {
                    const isError = e.type === 'error' || e.type === 'step_failed' || e.type === 'operation_failed';
                    return '<div class="event-item' + (isError ? ' event-error' : '') + '">' +
                        '<span class="event-time">' + new Date(e.timestamp).toLocaleTimeString() + '</span>' +
                        '<span class="event-type">' + e.type + '</span>' +
                        '<span class="event-message">' + e.message + '</span>' +
                    '</div>';
                }).join('')) +
            '</div>' +
        '</div>' +
        
        (buttons ? '<div class="action-buttons">' + buttons + '</div>' : '');
    
    // Restore scroll positions
    const newEventsList = container.querySelector('.events-list');
    if (newEventsList && eventsScrollTop > 0) {
        newEventsList.scrollTop = eventsScrollTop;
    }
    
    // Handle steps list scrolling
    const newStepsList = container.querySelector('.steps-list');
    if (newStepsList) {
        if (shouldAutoScroll) {
            // Find target step to scroll to
            const targetStep = newStepsList.querySelector('[data-step-index="' + currentStepIndex + '"]');
            if (targetStep) {
                // offsetTop is now relative to steps-list (position: relative)
                const stepTop = targetStep.offsetTop;
                // Center the step in the visible area
                const centerOffset = (newStepsList.clientHeight - targetStep.offsetHeight) / 2;
                newStepsList.scrollTop = Math.max(0, stepTop - centerOffset);
            }
        } else if (stepsScrollTop > 0) {
            // Restore previous scroll position
            newStepsList.scrollTop = stepsScrollTop;
        }
    }
}

// Start operation
async function startOperation(id) {
    try {
        const response = await fetch('/api/operations/' + id + '/start', { method: 'POST' });
        if (response.ok) {
            loadOperationDetail(id);
            loadOperations();
        } else {
            const data = await response.json();
            showAlertDialog(data.error);
        }
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Pause operation
async function pauseOperation(id) {
    const reason = prompt('Pause reason:');
    if (reason === null) return;
    
    try {
        const response = await fetch('/api/operations/' + id + '/pause', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ reason })
        });
        if (response.ok) {
            loadOperationDetail(id);
            loadOperations();
        } else {
            const data = await response.json();
            showAlertDialog(data.error);
        }
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Delete operation (only for operations that were never started)
async function deleteOperation(id) {
    const confirmed = await showConfirmDialog(
        'Are you sure you want to delete this operation? This cannot be undone.',
        'Delete Operation',
        'Delete'
    );
    if (!confirmed) {
        return;
    }
    
    try {
        const response = await fetch('/api/operations/' + id, { method: 'DELETE' });
        if (response.ok) {
            // Clear the detail view and stop polling
            selectedOperationId = null;
            if (pollInterval) {
                clearInterval(pollInterval);
                pollInterval = null;
            }
            document.getElementById('operation-detail').innerHTML = 
                '<div class="empty-state"><p>Select an operation to view details</p></div>';
            loadOperations();
        } else {
            const data = await response.json();
            showAlertDialog(data.error);
        }
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Show resume modal
function showResumeModal() {
    document.getElementById('resume-modal').classList.add('active');
}

// Close resume modal
function closeModal() {
    document.getElementById('resume-modal').classList.remove('active');
}

// Show timeout modal (production mode)
function showTimeoutModal(currentValue) {
    const modal = document.getElementById('timeout-modal');
    if (!modal) return;
    document.getElementById('timeout-value').value = currentValue;
    modal.classList.add('active');
}

// Close timeout modal
function closeTimeoutModal() {
    const modal = document.getElementById('timeout-modal');
    if (modal) modal.classList.remove('active');
}

// Submit timeout update
async function submitTimeout() {
    const timeout = parseInt(document.getElementById('timeout-value').value);
    if (!timeout || timeout < 60) {
        showAlertDialog('Timeout must be at least 60 seconds');
        return;
    }
    if (timeout > 7200) {
        showAlertDialog('Timeout cannot exceed 7200 seconds (2 hours)');
        return;
    }
    
    try {
        const response = await fetch('/api/operations/' + selectedOperationId, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ wait_timeout: timeout })
        });
        if (response.ok) {
            closeTimeoutModal();
            loadOperationDetail(selectedOperationId);
        } else {
            const data = await response.json();
            showAlertDialog(data.error);
        }
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Submit resume action
async function submitResume() {
    const action = document.getElementById('resume-action').value;
    const comment = document.getElementById('resume-comment').value;
    
    try {
        const response = await fetch('/api/operations/' + selectedOperationId + '/resume', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action, comment })
        });
        if (response.ok) {
            closeModal();
            loadOperationDetail(selectedOperationId);
            loadOperations();
        } else {
            const data = await response.json();
            showAlertDialog(data.error);
        }
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Create form submission handler
function setupCreateForm() {
    document.getElementById('create-form').addEventListener('submit', async function(e) {
        e.preventDefault();
        const type = document.getElementById('operation-type').value;
        
        // Get cluster ID from unified cluster-select element
        const clusterSelect = document.getElementById('cluster-select');
        const clusterId = clusterSelect ? clusterSelect.value : '';
        
        // Get region (production mode only)
        const regionSelect = document.getElementById('region-select');
        const region = regionSelect ? regionSelect.value : '';
        
        if (!clusterId) {
            showAlertDialog('Please select a cluster');
            return;
        }
        
        let params = {};
        if (type === 'instance_type_change') {
            params = { target_instance_type: document.getElementById('param-instance-type').value };
            const excludedInstances = getExcludedInstances();
            if (excludedInstances.length > 0) {
                params.exclude_instances = excludedInstances;
            }
            const skipTempInstance = document.getElementById('param-skip-temp-instance');
            if (skipTempInstance && skipTempInstance.checked) {
                params.skip_temp_instance = true;
            }
        } else if (type === 'engine_upgrade') {
            params = {
                target_engine_version: document.getElementById('param-engine-version').value
            };
            const paramGroup = document.getElementById('param-parameter-group').value;
            if (paramGroup) {
                params.db_cluster_parameter_group_name = paramGroup;
            }
        } else if (type === 'instance_cycle') {
            params = {};
            const excludedInstances = getExcludedInstances('-cycle');
            if (excludedInstances.length > 0) {
                params.exclude_instances = excludedInstances;
            }
            const skipTempInstance = document.getElementById('param-skip-temp-instance-cycle');
            if (skipTempInstance && skipTempInstance.checked) {
                params.skip_temp_instance = true;
            }
        }
        
        const body = { type, cluster_id: clusterId, params };
        if (region) body.region = region;
        
        try {
            const response = await fetch('/api/operations', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
            const data = await response.json();
            if (response.ok) {
                loadOperations();
                selectOperation(data.id);
            } else {
                showAlertDialog(data.error);
            }
        } catch (err) {
            showAlertDialog(err.message);
        }
    });
}

// Operation type change handler
function setupOperationTypeHandler() {
    document.getElementById('operation-type').addEventListener('change', function() {
        renderParams(this.value);
    });
}

// Initialize the application
function initApp() {
    setupCreateForm();
    setupOperationTypeHandler();
    loadOperations();
    setInterval(loadOperations, 10000);
    
    // Set up region/cluster handlers (same for both modes)
    const regionSelect = document.getElementById('region-select');
    if (regionSelect) {
        regionSelect.addEventListener('change', function() {
            currentRegion = this.value;
            if (this.value) {
                loadClusters(this.value);
            } else {
                const trigger = document.querySelector('[data-select-id="cluster-select"] .custom-select-trigger');
                if (trigger) {
                    trigger.disabled = true;
                    trigger.setAttribute('disabled', '');
                }
                updateCustomSelect('cluster-select', [{ value: '', text: 'Select a region first...' }], '', 'Select a region first...');
            }
        });
    }
    
    const clusterSelect = document.getElementById('cluster-select');
    if (clusterSelect) {
        clusterSelect.addEventListener('change', function() {
            fetchClusterInfo(this.value, currentRegion);
        });
    }
    
    // Load regions and start cluster status refresh (both modes use API)
    loadRegions();
    startClusterStatusRefresh();
    
    // Demo mode: additional initialization for demo controls
    if (typeof isDemoMode !== 'undefined' && isDemoMode) {
        initDemoMode();
    }
}

// Copy to clipboard utility
async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        // Show brief visual feedback
        const btn = event.target.closest('.btn-copy');
        if (btn) {
            btn.classList.add('copied');
            setTimeout(() => btn.classList.remove('copied'), 1500);
        }
    } catch (err) {
        console.error('Failed to copy:', err);
    }
}

// Run on DOM ready
document.addEventListener('DOMContentLoaded', initApp);
