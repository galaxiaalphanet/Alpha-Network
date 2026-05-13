"""
Alpha Network SDK — Connect AI agents to the Alpha Network blockchain.

Usage:
    from alpha_network_sdk import AlphaAgent, AlphaClient

    agent = AlphaAgent()
    agent.connect("https://alphanetx.xyz")
    agent.register()
    agent.start_earning()
    print(agent.balance())
"""

from .alpha_sdk import AlphaAgent, AlphaClient, AlphaAPIError, AlphaConnectionError

__version__ = "0.1.0"
__all__ = ["AlphaAgent", "AlphaClient", "AlphaAPIError", "AlphaConnectionError"]
