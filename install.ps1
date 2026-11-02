& {
	[CmdletBinding()]
	param(
		[string]$InstallDir
	)

	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue'

	if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
		throw 'Use install.sh on Linux or macOS.'
	}

	$architecture = $env:PROCESSOR_ARCHITEW6432
	if (-not $architecture) {
		$architecture = $env:PROCESSOR_ARCHITECTURE
	}

	if ($architecture -ne 'AMD64') {
		throw 'Prebuilt releases currently support amd64 only; build from source for this architecture.'
	}

	if (-not $InstallDir) {
		if (-not $env:USERPROFILE) {
			throw 'USERPROFILE is not set.'
		}

		$InstallDir = Join-Path $env:USERPROFILE '.bin'
	}

	$InstallDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($InstallDir)
	$destination = Join-Path $InstallDir 'builder.exe'
	if (Test-Path -LiteralPath $destination -PathType Container) {
		throw "$destination is a directory."
	}

	$tempDir = Join-Path $InstallDir ".builder-install-$([Guid]::NewGuid().ToString('N'))"
	$securityProtocol = [Net.ServicePointManager]::SecurityProtocol

	try {
		[Net.ServicePointManager]::SecurityProtocol = $securityProtocol -bor [Net.SecurityProtocolType]::Tls12
		$headers = @{ 'User-Agent' = 'builder-installer'; 'Accept' = 'application/vnd.github+json' }
		if ($env:GITHUB_TOKEN) {
			$headers['Authorization'] = "Bearer $env:GITHUB_TOKEN"
		}

		$release = Invoke-RestMethod -Uri 'https://api.github.com/repos/coalaura/builder/releases/latest' -Headers $headers
		if (-not $release.tag_name -or $release.draft -or $release.prerelease) {
			throw 'Could not resolve the latest stable release.'
		}

		$tag = [Uri]::EscapeDataString($release.tag_name)
		$asset = 'builder-windows-amd64.exe'
		$releaseUrl = "https://github.com/coalaura/builder/releases/download/$tag"
		[IO.Directory]::CreateDirectory($tempDir) | Out-Null
		$downloadPath = Join-Path $tempDir $asset
		$checksumPath = Join-Path $tempDir 'SHA256SUMS'

		Write-Host "Downloading builder $($release.tag_name)..."
		Invoke-WebRequest -UseBasicParsing -Uri "$releaseUrl/$asset" -OutFile $downloadPath
		Invoke-WebRequest -UseBasicParsing -Uri "$releaseUrl/SHA256SUMS" -OutFile $checksumPath

		$pattern = "^(?<hash>[0-9a-fA-F]{64}) [ *]$([regex]::Escape($asset))$"
		$checksums = @(Get-Content -LiteralPath $checksumPath | Where-Object { $_ -cmatch $pattern })
		if ($checksums.Count -ne 1) {
			throw "Missing or invalid checksum for $asset."
		}

		$expectedChecksum = [regex]::Match($checksums[0], $pattern).Groups['hash'].Value
		$actualChecksum = (Get-FileHash -LiteralPath $downloadPath -Algorithm SHA256).Hash
		if ($actualChecksum -ne $expectedChecksum) {
			throw "Checksum verification failed for $asset."
		}

		if ([IO.File]::Exists($destination)) {
			$backupPath = Join-Path $tempDir 'builder.previous.exe'
			[IO.File]::Replace($downloadPath, $destination, $backupPath)
		} else {
			[IO.File]::Move($downloadPath, $destination)
		}

		$userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
		$userEntries = @($userPath -split ';' | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).Trim().TrimEnd('\') })
		if ($userEntries -notcontains $InstallDir.TrimEnd('\')) {
			$userPath = "$($userPath.TrimEnd(';'));$InstallDir".TrimStart(';')
			[Environment]::SetEnvironmentVariable('Path', $userPath, 'User')
		}

		$processPath = [string]$env:PATH
		$processEntries = @($processPath -split ';' | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).Trim().TrimEnd('\') })
		if ($processEntries -notcontains $InstallDir.TrimEnd('\')) {
			$env:PATH = "$($processPath.TrimEnd(';'));$InstallDir".TrimStart(';')
		}
	} finally {
		[Net.ServicePointManager]::SecurityProtocol = $securityProtocol
		if (Test-Path -LiteralPath $tempDir) {
			Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
		}
	}

	Write-Host "Installed builder $($release.tag_name) to $destination"
	Write-Host 'Run builder --help to get started. New terminals will also have builder on PATH.'

	if ([Console]::IsInputRedirected) {
		Write-Host 'Skipping setup (no interactive terminal). Run builder setup later.'
		return
	}

	try {
		$answer = Read-Host 'Run builder setup now? [y/N]'
	} catch {
		Write-Host 'Skipping setup (interactive input is unavailable). Run builder setup later.'
		return
	}

	if ($answer -match '^\s*y(es)?\s*$') {
		& $destination setup
		if ($LASTEXITCODE -ne 0) {
			throw "builder setup failed with exit code $LASTEXITCODE. Builder remains installed at $destination."
		}
	}
} @args
