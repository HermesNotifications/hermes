# UsersApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**listUsers**](UsersApi.md#listUsers) | **GET** /v1/users | List users |


<a id="listUsers"></a>
# **listUsers**
> List&lt;UserItem&gt; listUsers(organizationId)

List users

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.UsersApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    UsersApi apiInstance = new UsersApi(defaultClient);
    String organizationId = "organizationId_example"; // String | Filter by organization ID
    try {
      List<UserItem> result = apiInstance.listUsers(organizationId);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling UsersApi#listUsers");
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
| **organizationId** | **String**| Filter by organization ID | [optional] |

### Return type

[**List&lt;UserItem&gt;**](UserItem.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **0** | Error |  -  |

