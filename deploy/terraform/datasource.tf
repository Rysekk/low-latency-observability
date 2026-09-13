# Source from https://registry.terraform.io/providers/oracle/oci/latest/docs/data-sources/identity_availability_domains

# Tenancy is the root or parent to all compartments.
# For this tutorial, use the value of <tenancy-ocid> for the compartment OCID.

data "oci_identity_availability_domains" "ads" {
  compartment_id = oci_identity_compartment.tf-compartment.id
}


data "oci_core_images" "ubuntu_image" {
  compartment_id           = var.tenancy_ocid
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "26.04"
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
  # DYNAMIC FILTER: Looks for "aarch64" (ARM) in the image name
  filter {
    name   = "display_name"
    values = ["^.*[Uu]buntu.*aarch64.*$"]
    regex  = true
  }
}