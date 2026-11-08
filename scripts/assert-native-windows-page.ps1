# Read the actual Win32 accessibility provider, independently of Go's model.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$window = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]::new([long]$env:GOPURR_ACCEPTANCE_HWND))
$condition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ControlTypeProperty, [System.Windows.Automation.ControlType]::TabItem)
$deadline = (Get-Date).AddSeconds(6)
do {
    $selected = @($window.FindAll([System.Windows.Automation.TreeScope]::Descendants, $condition) | Where-Object {
        ([System.Windows.Automation.SelectionItemPattern]$_.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern)).Current.IsSelected
    } | ForEach-Object { ($_.Current.AutomationId + ":" + $_.Current.Name) })
    if ($selected | Where-Object { $_.StartsWith("desktop-nav-$($env:GOPURR_ACCEPTANCE_PAGE):") }) { exit 0 }
    Start-Sleep -Milliseconds 150
} while ((Get-Date) -lt $deadline)
throw "Actual NSH navigation expected '$env:GOPURR_ACCEPTANCE_PAGE'; selected tabs: $($selected -join ', ')"
