<a id="readme-top"></a>

<!-- PROJECT SHIELDS -->
<div align="center">

[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![License][license-shield]][license-url]
[![GitHub Stars][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]

</div>
<br />
<div align="center">
  <a href="https://github.com/rubrionmc/wayguard">
    <img src=".github/images/logo.png" alt="Logo" width="135" height="135">
  </a>

<h3 align="center">Wayguard</h3>

  <p align="center">
    A lightweight Go TCP gateway that automatically routes traffic to a healthy backend.
    <br />
    <br />
    <a href="https://rubrion.net/docs/howto/join">View Demo</a>
    &middot;
    <a href="https://github.com/rubrionmc/wayguard/issues/new?labels=bug&template=bug-report.md">Report Bug</a>
    &middot;
    <a href="https://github.com/rubrionmc/wayguard/issues/new?labels=enhancement&template=feature-request.md">Request Feature</a>
  </p>
</div>

## Table of Contents

<details>
  <summary>Contents</summary>
  <ol>
    <li><a href="#about-the-project">About The Project</a></li>
    <li><a href="#getting-started">Getting Started</a></li>
    <li><a href="#configuration">Configuration</a></li>
    <li><a href="#usage">Usage</a></li>
    <li><a href="#roadmap">Roadmap</a></li>
    <li><a href="#contributing">Contributing</a></li>
    <li><a href="#license">License</a></li>
    <li><a href="#contact">Contact</a></li>
  </ol>
</details>

## About The Project

**Wayguard** is a lightweight Go TCP proxy/gateway that:

- Discover healthy backends by there **labels** and **namespace** from k8s API.
- Routes traffic to a **single selected backend** automatically.
- Supports **Primary / Fallback backends** (e.g., different Pod types in Kubernetes).
- Performs **dynamic health checks** to detect and switch to healthy backends.
- Fully configurable via a TOML file.

The proxy ensures that **only one backend is active at a time** while keeping traffic uninterrupted if that one ist crashing.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Getting Started

Follow these steps to run the proxy locally or in a Kubernetes environment.

### Prerequisites

* Go 1.21+ installed
* Git
* k8s cluster with API access (k8s, kind, minikube, etc.)
* Optional: Docker for containerized deployments

### Installation

1. Clone the repository:

```bash
git clone https://github.com/rubrionmc/wayguard.git
cd wayguard
````

2. Prepare the configuration file `config.toml` or specify a custom path with `-config`/`-c`.

3. Build the proxy:

```bash
go mod download
go build -o wayguard .
```

or use the provided Dockerfile to build a container image:
```bash
docker build -t wayguard:latest .
```

or use the deploy.sh to deploy the proxy directly to a local Kubernetes cluster:
```bash
sh deploy.sh
```

3. Prepare the configuration file `config.toml` or specify a custom path with `-config`/`-c`.

---
<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Usage

Run the proxy with:

```bash
./gopolice -config config.toml
```
Or use the docker image you built

* Logs show backend health and active connections.
* Only one backend is selected at any time.
* Automatically switches to fallback if primary becomes unavailable.

### Example Logs

```
comming soon
```

---

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Roadmap

* [x] Configurable Primary/Fallback backends
* [x] Health checks and dynamic selected connection
* [x] Lightweight architecture with minimal dependencies
* [x] Kubernetes API integration for automatic Pod discovery
* [ ] TLS support
* [ ] Metrics endpoint

<p align="right">(<a href="#readme-top">back to top</a>)</p>

---

## Contributing

Contributions are welcome!

1. Fork the repository
2. Create your feature branch (`git checkout -b octodex/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing Feature'`)
4. Push to the branch (`git push origin octodex/amazing-feature`)
5. Open a Pull Request

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## License

Distributed under the RPL License. See `LICENSE` for details.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

<!-- SHIELDS -->

[contributors-shield]: https://img.shields.io/github/contributors/rubrionmc/wayguard.svg?style=for-the-badge
[contributors-url]: https://github.com/rubrionmc/wayguard/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/rubrionmc/wayguard.svg?style=for-the-badge
[forks-url]: https://github.com/rubrionmc/wayguard/network/members
[license-shield]: https://img.shields.io/badge/License-RPL-blue.svg?style=for-the-badge
[license-url]: https://github.com/rubrionmc/wayguard/blob/main/LICENSE.txt
[stars-shield]: https://img.shields.io/github/stars/rubrionmc/wayguard?style=for-the-badge
[stars-url]: https://github.com/rubrionmc/wayguard/stargazers
[issues-shield]: https://img.shields.io/github/issues/rubrionmc/wayguard?style=for-the-badge
[issues-url]: https://github.com/rubrionmc/wayguard/issues