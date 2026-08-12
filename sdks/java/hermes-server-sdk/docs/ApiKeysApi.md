# ApiKeysApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createApiKey**](ApiKeysApi.md#createApiKey) | **POST** /v1/apikeys | Create a new API key |
| [**deleteApiKey**](ApiKeysApi.md#deleteApiKey) | **DELETE** /v1/apikeys/{id} | Revoke an API key |
| [**listApiKeys**](ApiKeysApi.md#listApiKeys) | **GET** /v1/apikeys | List all API keys |
| [**setApiKeyRateLimit**](ApiKeysApi.md#setApiKeyRateLimit) | **PUT** /v1/apikeys/{id}/rate-limit | Set or clear a key&#39;s rate limit |


<a id="createApiKey"></a>
# **createApiKey**
> ApiKeyCreatedOutputBody createApiKey(createAPIKeyInputBody)

Create a new API key

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.ApiKeysApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    ApiKeysApi apiInstance = new ApiKeysApi(defaultClient);
    CreateAPIKeyInputBody createAPIKeyInputBody = new CreateAPIKeyInputBody(); // CreateAPIKeyInputBody | 
    try {
      ApiKeyCreatedOutputBody result = apiInstance.createApiKey(createAPIKeyInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling ApiKeysApi#createApiKey");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **createAPIKeyInputBody** | [**CreateAPIKeyInputBody**](CreateAPIKeyInputBody.md)|  | |

### Return type

[**ApiKeyCreatedOutputBody**](ApiKeyCreatedOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **201** | Created |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

<a id="deleteApiKey"></a>
# **deleteApiKey**
> deleteApiKey(id)

Revoke an API key

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.ApiKeysApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    ApiKeysApi apiInstance = new ApiKeysApi(defaultClient);
    String id = "id_example"; // String | API key ID
    try {
      apiInstance.deleteApiKey(id);
    } catch (ApiException e) {
      System.err.println("Exception when calling ApiKeysApi#deleteApiKey");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **id** | **String**| API key ID | |

### Return type

null (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **204** | No Content |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

<a id="listApiKeys"></a>
# **listApiKeys**
> List&lt;ApiKeyView&gt; listApiKeys()

List all API keys

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.ApiKeysApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    ApiKeysApi apiInstance = new ApiKeysApi(defaultClient);
    try {
      List<ApiKeyView> result = apiInstance.listApiKeys();
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling ApiKeysApi#listApiKeys");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**List&lt;ApiKeyView&gt;**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

<a id="setApiKeyRateLimit"></a>
# **setApiKeyRateLimit**
> ApiKeyView setApiKeyRateLimit(id, setAPIKeyRateLimitInputBody)

Set or clear a key&#39;s rate limit

Replaces this key&#39;s rate limit. Omitted fields reset to the service default, so an empty body clears the override entirely. Takes effect within the API key cache TTL; this endpoint invalidates that entry so the change is immediate.

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.ApiKeysApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    ApiKeysApi apiInstance = new ApiKeysApi(defaultClient);
    String id = "id_example"; // String | API key ID
    SetAPIKeyRateLimitInputBody setAPIKeyRateLimitInputBody = new SetAPIKeyRateLimitInputBody(); // SetAPIKeyRateLimitInputBody | 
    try {
      ApiKeyView result = apiInstance.setApiKeyRateLimit(id, setAPIKeyRateLimitInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling ApiKeysApi#setApiKeyRateLimit");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **id** | **String**| API key ID | |
| **setAPIKeyRateLimitInputBody** | [**SetAPIKeyRateLimitInputBody**](SetAPIKeyRateLimitInputBody.md)|  | |

### Return type

[**ApiKeyView**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
| **0** | Error |  -  |

