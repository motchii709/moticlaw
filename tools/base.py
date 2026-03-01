from abc import ABC, abstractmethod
from typing import Any, Dict

class BaseTool(ABC):
    """Base class for all Moticlaw tools."""
    
    @property
    @abstractmethod
    def name(self) -> str:
        """The identifier of the tool."""
        pass

    @property
    @abstractmethod
    def description(self) -> str:
        """A brief description of what the tool does."""
        pass

    @property
    @abstractmethod
    def parameters_schema(self) -> Dict[str, Any]:
        """JSON schema for the tool's parameters."""
        pass

    @abstractmethod
    async def execute(self, **kwargs) -> Any:
        """Executes the tool logic."""
        pass
