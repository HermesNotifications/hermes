# AuthApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**exchangeToken**](AuthApi.md#exchangeToken) | **POST** /v1/auth/token | Exchange credentials for a user JWT token |


<a id="exchangeToken"></a>
# **exchangeToken**
> TokenOutputBody exchangeToken(tokenInputBody)

Exchange credentials for a user JWT token

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.AuthApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    AuthApi apiInstance = new AuthApi(defaultClient);
    TokenInputBody tokenInputBody = new TokenInputBody(); // TokenInputBody | 
    try {
      TokenOutputBody result = apiInstance.exchangeToken(tokenInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling AuthApi#exchangeToken");
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
| **tokenInputBody** | [**TokenInputBody**](TokenInputBody.md)|  | |

### Return type

[**TokenOutputBody**](TokenOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **0** | Error |  -  |

