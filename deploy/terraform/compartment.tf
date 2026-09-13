resource "oci_identity_compartment" "tf-compartment" {
    # Required
    compartment_id = var.tenancy_ocid
    description = "Compartment for Terraform resources of the trading app"
    name = "trading-app-compartment"
}