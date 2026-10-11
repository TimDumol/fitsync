class GarminConnectConnectionError(Exception):
    """Raised when communication ended in error."""


class GarminConnectTransportError(GarminConnectConnectionError):
    """Raised when the required fingerprinted HTTP transport is unavailable."""


class GarminConnectTooManyRequestsError(Exception):
    """Raised when rate limit is exceeded."""


class GarminConnectAuthenticationError(Exception):
    """Raised when authentication is failed."""


class GarminConnectInvalidFileFormatError(Exception):
    """Raised when an invalid file format is provided."""
