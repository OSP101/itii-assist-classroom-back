param(
    [string]$BaseUrl = 'http://127.0.0.1:8000',
    [Parameter(Mandatory = $true)]
    [string]$Username,
    [Parameter(Mandatory = $true)]
    [string]$Password,
    [int]$StudentCount = 800,
    [int[]]$ConcurrencyLevels = @(50, 100, 150, 200, 250, 300, 400, 500),
    [string]$CourseId = '',
    [int]$SectionId = 0,
    [string]$ClassroomId = '',
    [int]$DeskNumber = 4,
    [switch]$KeepGoingAfterFailure
)

$ErrorActionPreference = 'Stop'

function Invoke-JsonApi {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Method,
        [Parameter(Mandatory = $true)]
        [string]$Uri,
        [hashtable]$Headers,
        $Body
    )

    $invokeParams = @{
        Uri     = $Uri
        Method  = $Method
        Headers = $Headers
    }

    if ($null -ne $Body) {
        $invokeParams.ContentType = 'application/json'
        $invokeParams.Body = ($Body | ConvertTo-Json -Depth 12 -Compress)
    }

    Invoke-RestMethod @invokeParams
}

function Split-Batches {
    param(
        [Parameter(Mandatory = $true)]
        [object[]]$Items,
        [int]$BatchSize = 200
    )

    $batches = @()
    for ($index = 0; $index -lt $Items.Count; $index += $BatchSize) {
        $end = [Math]::Min($index + $BatchSize - 1, $Items.Count - 1)
        $batches += ,@($Items[$index..$end])
    }
    return $batches
}

function New-StudentCode {
    param([int]$Index)
    $value = [long]78000000000 + $Index
    return ('{0:D11}' -f $value)
}

function Parse-LoadTestOutput {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Output,
        [Parameter(Mandatory = $true)]
        [string]$Scenario,
        [Parameter(Mandatory = $true)]
        [int]$Concurrency
    )

    $text = $Output -join "`n"
    $success = [int]([regex]::Match($text, 'Success:\s+(\d+)').Groups[1].Value)
    $failures = [int]([regex]::Match($text, 'Failures:\s+(\d+)').Groups[1].Value)
    $throughput = [double]([regex]::Match($text, 'Throughput:\s+([0-9.]+)').Groups[1].Value)
    $p95 = [double]([regex]::Match($text, 'p95=([0-9.]+)').Groups[1].Value)
    $p99 = [double]([regex]::Match($text, 'p99=([0-9.]+)').Groups[1].Value)
    $max = [double]([regex]::Match($text, 'max=([0-9.]+)').Groups[1].Value)
    $statusCodes = [regex]::Match($text, 'Status codes:\s+(.+)').Groups[1].Value.Trim()
    $errors = [regex]::Match($text, 'Errors:\s+(.+)').Groups[1].Value.Trim()
    $hardFailure = $failures -gt 0 -or $text.Contains('actively refused')

    [pscustomobject]@{
        Scenario     = $Scenario
        Concurrency  = $Concurrency
        Success      = $success
        Failures     = $failures
        Throughput   = [Math]::Round($throughput, 2)
        P95Ms        = [Math]::Round($p95, 2)
        P99Ms        = [Math]::Round($p99, 2)
        MaxMs        = [Math]::Round($max, 2)
        StatusCodes  = $statusCodes
        Errors       = $errors
        HardFailure  = $hardFailure
        RawOutput    = $text
    }
}

function Write-BodyFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string[]]$Lines
    )

    $directory = Split-Path -Parent $Path
    if (-not (Test-Path $directory)) {
        New-Item -ItemType Directory -Path $directory | Out-Null
    }
    [System.IO.File]::WriteAllLines($Path, $Lines)
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$tempRoot = Join-Path $env:TEMP 'itii-assist-stress'
New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
$loadtestExe = Join-Path $tempRoot 'loadtest.exe'

Push-Location $repoRoot
try {
    go build -o $loadtestExe ./tools/loadtest

    $login = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/auth/login" -Body @{ username = $Username; password = $Password }
    $headers = @{ Authorization = "Bearer $($login.data.accessToken)" }

    if ([string]::IsNullOrWhiteSpace($CourseId)) {
        $courses = Invoke-JsonApi -Method 'GET' -Uri "$BaseUrl/api/courses?page=1&limit=1" -Headers $headers
        $CourseId = $courses.data.courses[0].id
        if ($SectionId -le 0) {
            $SectionId = [int]$courses.data.courses[0].sections[0].id
        }
    }

    if ($SectionId -le 0) {
        throw 'SectionId is required if it cannot be inferred from the first course.'
    }

    if ([string]::IsNullOrWhiteSpace($ClassroomId)) {
        $classrooms = Invoke-JsonApi -Method 'GET' -Uri "$BaseUrl/api/classrooms" -Headers $headers
        $ClassroomId = $classrooms.data.classrooms[0].id
    }

    $studentRows = New-Object System.Collections.Generic.List[object]
    $studentCodes = New-Object System.Collections.Generic.List[string]
    for ($index = 1; $index -le $StudentCount; $index++) {
        $studentCode = New-StudentCode -Index $index
        $studentCodes.Add($studentCode)
        $studentRows.Add([pscustomobject]@{
            student_id = $studentCode
            full_name  = "Load Test Student $index"
            email      = "loadtest+$studentCode@example.com"
        })
    }

    foreach ($batch in Split-Batches -Items $studentRows.ToArray() -BatchSize 200) {
        $null = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/students/import" -Headers $headers -Body @{ students = $batch }
    }

    $studentIdMap = @{}
    foreach ($batch in Split-Batches -Items $studentCodes.ToArray() -BatchSize 200) {
        $searchResult = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/students/search-by-ids" -Headers $headers -Body @{ studentIds = $batch }
        foreach ($student in @($searchResult.data)) {
            $studentIdMap[$student.student_id] = [int]$student.id
        }
    }

    $studentDbIds = @()
    foreach ($studentCode in $studentCodes) {
        if (-not $studentIdMap.ContainsKey($studentCode)) {
            throw "Student $studentCode was not returned by /api/students/search-by-ids"
        }
        $studentDbIds += $studentIdMap[$studentCode]
    }

    foreach ($batch in Split-Batches -Items $studentDbIds -BatchSize 200) {
        $null = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/courses/$CourseId/sections/$SectionId/students/bulk" -Headers $headers -Body @{ student_ids = $batch }
    }

    $results = New-Object System.Collections.Generic.List[object]

    $attendanceFailed = $false
    $queueFailed = $false

    foreach ($concurrency in $ConcurrencyLevels) {
        if ($concurrency -gt $StudentCount) {
            break
        }

        $now = Get-Date
        if (-not $attendanceFailed -or $KeepGoingAfterFailure) {
            $attendancePin = ('{0:D6}' -f ((Get-Random -Minimum 100000 -Maximum 999999)))
            $attendance = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/attendance" -Headers $headers -Body @{
                course_id = $CourseId
                section_ids = @($SectionId)
                title = "stress-attendance-$concurrency-$(Get-Date -Format 'HHmmss')"
                pin_code = $attendancePin
                session_type = 'lecture'
                check_location = $false
                radius_meters = 50
                start_time = $now.AddMinutes(-5).ToString('o')
                end_time = $now.AddMinutes(90).ToString('o')
                late_threshold_minutes = 15
            }
            $attendanceSessionId = $attendance.data.id

            $attendanceBodies = for ($index = 0; $index -lt $concurrency; $index++) {
                @{ student_id = $studentDbIds[$index]; pin_code = $attendancePin } | ConvertTo-Json -Compress
            }
            $attendanceFile = Join-Path $tempRoot "attendance-$concurrency.ndjson"
            Write-BodyFile -Path $attendanceFile -Lines $attendanceBodies
            $attendanceOutput = & $loadtestExe -name "attendance-$concurrency" -url "$BaseUrl/api/attendance/check-in/$attendanceSessionId" -method POST -body-file $attendanceFile -header 'Content-Type: application/json' -concurrency $concurrency -requests $concurrency
            $attendanceSummary = Parse-LoadTestOutput -Output $attendanceOutput -Scenario 'attendance' -Concurrency $concurrency
            $results.Add($attendanceSummary)
            if ($attendanceSummary.HardFailure) {
                $attendanceFailed = $true
            }
        }

        if (-not $queueFailed -or $KeepGoingAfterFailure) {
            $queue = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/courses/$CourseId/queue/sessions" -Headers $headers -Body @{
                classroom_id = $ClassroomId
                title = "stress-queue-$concurrency-$(Get-Date -Format 'HHmmss')"
                description = 'automated stress test'
                require_attendance = $false
            }
            $queueSessionId = $queue.data.id
            $queuePin = $queue.data.pin_code
            $null = Invoke-JsonApi -Method 'POST' -Uri "$BaseUrl/api/courses/$CourseId/queue/sessions/$queueSessionId/start" -Headers $headers

            $queueBodies = for ($index = 0; $index -lt $concurrency; $index++) {
                @{ pin_code = $queuePin; student_id = $studentCodes[$index]; desk_number = $DeskNumber; booking_type = 'help' } | ConvertTo-Json -Compress
            }
            $queueFile = Join-Path $tempRoot "queue-$concurrency.ndjson"
            Write-BodyFile -Path $queueFile -Lines $queueBodies
            $queueOutput = & $loadtestExe -name "queue-$concurrency" -url "$BaseUrl/api/queue/bookings" -method POST -body-file $queueFile -header 'Content-Type: application/json' -concurrency $concurrency -requests $concurrency
            $queueSummary = Parse-LoadTestOutput -Output $queueOutput -Scenario 'queue' -Concurrency $concurrency
            $results.Add($queueSummary)
            if ($queueSummary.HardFailure) {
                $queueFailed = $true
            }
        }

        if (-not $KeepGoingAfterFailure) {
            if ($attendanceFailed -and $queueFailed) {
                break
            }
        }
    }

    foreach ($result in $results) {
        $line = "scenario=$($result.Scenario) concurrency=$($result.Concurrency) success=$($result.Success) failures=$($result.Failures) throughput=$($result.Throughput) p95_ms=$($result.P95Ms) p99_ms=$($result.P99Ms) max_ms=$($result.MaxMs) status_codes=$($result.StatusCodes)"
        if (-not [string]::IsNullOrWhiteSpace($result.Errors)) {
            $line += " errors=$($result.Errors)"
        }
        Write-Output $line
    }
}
finally {
    Pop-Location
}