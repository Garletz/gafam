## Goal

The goal of GAFAM is to provide a highly secure, private cloud space allowing you to:
- Receive and manage administrative SMS verification codes without relying on a physical phone.
- Manage contacts and synchronize data directly to your own personal SQL server.
- Automatically deploy a fully isolated backend (VPC) on cloud providers like your own linux server/cloudron behind your box IPV4 or cloud provider DigitalOcean AWS GCP with a classical VPC ect...
- Access your data from anywhere via a secure, global 3D command deck.

## Architecture Overview

The system is composed of three main parts:
1. The Personal VPC: A self-hosted Dockerized backend containing an SQL database that permanently saves and synchronizes your SMS and contacts.
2. Android Relay Agent: A minimal app installed on your physical device that intercepts incoming SMS and pushes them exclusively to your personal VPC.
3. Global Frontend: A web-based visual command deck that connects directly to your personal VPC to read your data.
