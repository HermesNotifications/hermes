# OrganizationsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createOrganization**](OrganizationsApi.md#createOrganization) | **POST** /v1/organizations | Create an organization |
| [**listOrganizations**](OrganizationsApi.md#listOrganizations) | **GET** /v1/organizations | List organizations |


<a id="createOrganization"></a>
# **createOrganization**
> OrganizationItem createOrganization(createOrganizationInputBody)

Create an organization

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.OrganizationsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    OrganizationsApi apiInstance = new OrganizationsApi(defaultClient);
    CreateOrganizationInputBody createOrganizationInputBody = new CreateOrganizationInputBody(); // CreateOrganizationInputBody | 
    try {
      OrganizationItem result = apiInstance.createOrganization(createOrganizationInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling OrganizationsApi#createOrganization");
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
| **createOrganizationInputBody** | [**CreateOrganizationInputBody**](CreateOrganizationInputBody.md)|  | |

### Return type

[**OrganizationItem**](OrganizationItem.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **201** | Created |  -  |
| **0** | Error |  -  |

<a id="listOrganizations"></a>
# **listOrganizations**
> List&lt;OrganizationItem&gt; listOrganizations()

List organizations

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.OrganizationsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    OrganizationsApi apiInstance = new OrganizationsApi(defaultClient);
    try {
      List<OrganizationItem> result = apiInstance.listOrganizations();
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling OrganizationsApi#listOrganizations");
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

[**List&lt;OrganizationItem&gt;**](OrganizationItem.md)

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

