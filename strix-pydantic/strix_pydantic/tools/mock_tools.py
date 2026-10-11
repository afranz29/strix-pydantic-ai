"""Mock tools for testing orchestrator tool dispatch without Docker backend."""

import json
import logging
from typing import Any

logger = logging.getLogger(__name__)


async def scan_ports(target: str, ports: str | None = None) -> dict[str, Any]:
    """
    Mock port scanning tool.

    Args:
        target: Target host/URL
        ports: Comma-separated port list (default: common ports)

    Returns:
        Dict with open ports and services
    """
    logger.info(f"🔍 [MOCK] scan_ports({target}, {ports})")

    # Simulate port scan results
    results = {
        "target": target,
        "scan_time": "2.3s",
        "open_ports": [
            {"port": 80, "service": "http", "version": "Apache 2.4"},
            {"port": 443, "service": "https", "version": "Apache 2.4"},
            {"port": 22, "service": "ssh", "version": "OpenSSH 7.4"},
        ],
        "closed_ports": [21, 25, 23],
        "filtered_ports": [3306, 5432],
    }

    logger.info(f"✅ [MOCK] scan_ports returned {len(results['open_ports'])} open ports")
    return results


async def check_vulnerability(target: str, vuln_type: str | None = None) -> dict[str, Any]:
    """
    Mock vulnerability checking tool.

    Args:
        target: Target host/URL
        vuln_type: Vulnerability type to check (sql_injection, xss, etc)

    Returns:
        Dict with vulnerability findings
    """
    logger.info(f"🔍 [MOCK] check_vulnerability({target}, {vuln_type})")

    # Simulate vulnerability findings
    findings = {
        "target": target,
        "scan_type": vuln_type or "general",
        "vulnerabilities": [
            {
                "id": "CVE-2021-1234",
                "type": "sql_injection",
                "severity": "high",
                "description": "SQL injection in login form",
                "parameter": "username",
                "poc": "' OR '1'='1",
            },
            {
                "id": "CVE-2020-5678",
                "type": "xss",
                "severity": "medium",
                "description": "Reflected XSS in search",
                "parameter": "q",
                "poc": "<script>alert('XSS')</script>",
            },
        ],
        "scan_time": "5.2s",
    }

    logger.info(f"✅ [MOCK] check_vulnerability found {len(findings['vulnerabilities'])} issues")
    return findings


async def gather_info(target: str) -> dict[str, Any]:
    """
    Mock information gathering tool.

    Args:
        target: Target host/URL

    Returns:
        Dict with target information
    """
    logger.info(f"🔍 [MOCK] gather_info({target})")

    # Simulate reconnaissance results
    info = {
        "target": target,
        "technologies": [
            {"name": "Apache", "version": "2.4.41"},
            {"name": "PHP", "version": "7.4.3"},
            {"name": "MySQL", "version": "5.7.32"},
        ],
        "dns_records": {
            "A": ["192.168.1.100"],
            "CNAME": ["example.com"],
            "MX": ["mail.example.com"],
        },
        "subdomains": [
            "www.example.com",
            "api.example.com",
            "admin.example.com",
        ],
        "headers": {
            "Server": "Apache/2.4.41 (Ubuntu)",
            "X-Powered-By": "PHP/7.4.3",
        },
    }

    logger.info(f"✅ [MOCK] gather_info found {len(info['technologies'])} technologies")
    return info


async def test_authentication(target: str, method: str = "basic") -> dict[str, Any]:
    """
    Mock authentication testing tool.

    Args:
        target: Target host/URL
        method: Authentication method to test

    Returns:
        Dict with authentication test results
    """
    logger.info(f"🔍 [MOCK] test_authentication({target}, {method})")

    results = {
        "target": target,
        "method": method,
        "findings": [
            {
                "issue": "default_credentials",
                "severity": "critical",
                "description": "Default credentials detected (admin:admin)",
                "affected_service": "admin panel",
            },
            {
                "issue": "weak_password_policy",
                "severity": "high",
                "description": "No minimum password length enforced",
            },
        ],
        "test_count": 15,
        "failures": 2,
    }

    logger.info(f"✅ [MOCK] test_authentication found {len(results['findings'])} issues")
    return results


# Registry of mock tools
MOCK_TOOLS = {
    "scan_ports": {
        "name": "scan_ports",
        "description": "Scan target for open ports and services",
        "callable": scan_ports,
        "contexts": {"parent"},  # Available in parent context
    },
    "check_vulnerability": {
        "name": "check_vulnerability",
        "description": "Check target for known vulnerabilities",
        "callable": check_vulnerability,
        "contexts": {"parent"},
    },
    "gather_info": {
        "name": "gather_info",
        "description": "Gather reconnaissance information about target",
        "callable": gather_info,
        "contexts": {"parent"},
    },
    "test_authentication": {
        "name": "test_authentication",
        "description": "Test authentication mechanisms",
        "callable": test_authentication,
        "contexts": {"parent"},
    },
}


def register_mock_tools(tool_registry) -> None:
    """
    Register all mock tools with the tool registry.

    Args:
        tool_registry: ToolRegistry instance to register tools with
    """
    logger.info(f"📦 Registering {len(MOCK_TOOLS)} mock tools")

    for tool_name, tool_info in MOCK_TOOLS.items():
        tool_registry.register_tool(
            name=tool_info["name"],
            description=tool_info["description"],
            callable_obj=tool_info["callable"],
            contexts=tool_info["contexts"],
        )
        logger.info(f"  ✅ Registered mock tool: {tool_name}")
