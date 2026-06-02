# GAFAM

GAFAM is a Distributed Personal Virtual Private Cloud (VPC) and communication command center.

## Vision

Modern life forces users to maintain physical smartphones, SIM cards, and static phone numbers to receive short-term administrative SMS codes and verifications. GAFAM virtualizes and secures this dependency through a distributed, self-hosted architecture.

Instead of relying on centralized servers to hold your data, GAFAM empowers every user to host their own secure backend. By decoupling your telecommunication data from your physical device, you regain true digital independence.

## Goal

The goal of GAFAM is to provide a highly secure, private cloud space allowing you to:
- Receive and manage administrative SMS verification codes without relying on a physical phone.
- Manage contacts and synchronize data directly to your own personal SQL server.
- Automatically deploy a fully isolated backend (VPC) on cloud providers like DigitalOcean.
- Access your data from anywhere via a secure, global 3D command deck.

## Architecture Overview

The system is composed of three main parts:
1. The Personal VPC: A self-hosted Dockerized backend containing an SQL database that permanently saves and synchronizes your SMS and contacts.
2. Android Relay Agent: A minimal app installed on your physical device that intercepts incoming SMS and pushes them exclusively to your personal VPC.
3. Global Frontend: A web-based visual command deck that connects directly to your personal VPC to read your data.
